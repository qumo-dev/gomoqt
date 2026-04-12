package moqt

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

// newAnnouncementWriter creates a new AnnouncementWriter for the given stream and prefix.
func newAnnouncementWriter(stream transport.Stream, prefix prefix, logger *slog.Logger) *AnnouncementWriter {
	if !isValidPrefix(prefix) {
		panic("invalid prefix for AnnouncementWriter")
	}

	sas := &AnnouncementWriter{
		prefix:   prefix,
		stream:   stream,
		ctx:      context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeAnnounce),
		actives:  make(map[suffix]*activeAnnouncement),
		initDone: make(chan struct{}),
		logger:   logger,
	}

	return sas
}

// AnnouncementWriter manages the sending of announcements for a specified prefix.
// It handles initialization, sending active announcements, and cleanup.
type AnnouncementWriter struct {
	prefix prefix
	stream transport.Stream
	ctx    context.Context
	logger *slog.Logger

	mu      sync.RWMutex
	actives map[suffix]*activeAnnouncement

	initDone chan struct{}
	initOnce sync.Once
	initErr  error
}

func (aw *AnnouncementWriter) logError(msg string, err error, args ...any) {
	if aw == nil || err == nil {
		return
	}
	if aw.logger != nil {
		aw.logger.Error(msg, append(args, "error", err)...)
	}
}

// init snapshots the currently active announcements, sends an ACTIVE AnnounceMessage
// for each active track suffix on the announce stream, and sets up end handlers.
func (aw *AnnouncementWriter) init(announcements map[*Announcement]struct{}) error {
	var err error
	aw.initOnce.Do(func() {
		defer close(aw.initDone)

		if aw.ctx.Err() != nil {
			err = Cause(aw.ctx)
			aw.mu.Lock()
			aw.initErr = err
			aw.mu.Unlock()
			return
		}

		actives := make(map[suffix]*activeAnnouncement)

		for ann := range announcements {
			if !ann.IsActive() {
				continue
			}
			sfx, ok := ann.BroadcastPath().GetSuffix(aw.prefix)
			if !ok {
				continue
			}
			// Always replace with the latest active announcement for the suffix.
			actives[sfx] = &activeAnnouncement{announcement: ann}
		}

		aw.mu.Lock()
		aw.actives = actives
		aw.mu.Unlock()

		for sfx, active := range actives {
			err = message.AnnounceMessage{
				AnnounceStatus:      message.ACTIVE,
				BroadcastPathSuffix: sfx,
				Hops:                uint64(active.announcement.Hops() + 1),
			}.Encode(aw.stream)
			if err != nil {
				if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
					err = &AnnounceError{StreamError: strErr}
				}
				aw.mu.Lock()
				aw.initErr = err
				aw.mu.Unlock()
				return
			}

			aw.mu.Lock()
			aw.registerEndHandler(sfx, active.announcement)
			aw.mu.Unlock()
		}
	})

	aw.mu.RLock()
	defer aw.mu.RUnlock()
	if err != nil {
		return err
	}
	return aw.initErr
}

// registerEndHandler registers handlers for when the announcement ends.
// It sets up AfterFunc to clean up when the announcement becomes inactive.
// Caller MUST hold aw.mu.
func (aw *AnnouncementWriter) registerEndHandler(sfx suffix, ann *Announcement) {
	stop := ann.AfterFunc(func() {
		aw.mu.Lock()
		defer aw.mu.Unlock()
		current, exists := aw.actives[sfx]
		if exists && current.announcement == ann {
			delete(aw.actives, sfx)
			err := message.AnnounceMessage{
				AnnounceStatus:      message.ENDED,
				BroadcastPathSuffix: sfx,
			}.Encode(aw.stream)
			if err != nil {
				aw.logError("failed to encode ANNOUNCE end message", err)
			}
		}
	})

	aw.actives[sfx].end = func() {
		if !stop() {
			return
		}
		aw.mu.Lock()
		defer aw.mu.Unlock()
		delete(aw.actives, sfx)
		if err := (message.AnnounceMessage{
			AnnounceStatus:      message.ENDED,
			BroadcastPathSuffix: sfx,
		}).Encode(aw.stream); err != nil {
			aw.logError("failed to encode ANNOUNCE end message", err)
		}
	}
}

// SendAnnouncement sends an announcement if it's active and not already sent.
// It replaces any existing announcement for the same suffix.
func (aw *AnnouncementWriter) SendAnnouncement(announcement *Announcement) error {
	// Wait for initialization to complete
	select {
	case <-aw.initDone:
		// Initialization complete
	case <-aw.ctx.Done():
		return Cause(aw.ctx)
	}

	aw.mu.RLock()
	initErr := aw.initErr
	aw.mu.RUnlock()
	if initErr != nil {
		return initErr
	}

	if !announcement.IsActive() {
		return nil // No need to send inactive announcements
	}

	// Get suffix for this announcement
	suffix, ok := announcement.BroadcastPath().GetSuffix(aw.prefix)
	if !ok {
		return errors.New("moq: broadcast path with invalid prefix")
	}

	aw.mu.Lock()

	active, exists := aw.actives[suffix]
	if exists && active.announcement == announcement {
		aw.mu.Unlock()
		return nil // Already active, no need to re-announce
	}

	// If there's an existing announcement for this suffix, end it first.
	// Release lock before calling end() since it acquires the lock internally.
	var endFunc func()
	if exists && active.end != nil {
		endFunc = active.end
	}
	aw.mu.Unlock()

	if endFunc != nil {
		endFunc()
	}

	aw.mu.Lock()
	defer aw.mu.Unlock()

	// Encode and send ACTIVE announcement
	err := message.AnnounceMessage{
		AnnounceStatus:      message.ACTIVE,
		BroadcastPathSuffix: suffix,
		Hops:                uint64(announcement.Hops() + 1),
	}.Encode(aw.stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return &AnnounceError{
				StreamError: strErr,
			}
		}

		return err
	}

	aw.actives[suffix] = &activeAnnouncement{announcement: announcement}
	aw.registerEndHandler(suffix, announcement)

	return nil
}

// Close gracefully closes the AnnouncementWriter and ends all active announcements.
func (aw *AnnouncementWriter) Close() error {
	aw.mu.Lock()
	// Collect end functions to call after releasing the lock
	var endFuncs []func()
	for _, active := range aw.actives {
		if active.end != nil {
			endFuncs = append(endFuncs, active.end)
		}
	}
	aw.actives = nil
	aw.mu.Unlock()

	// Call end functions without holding the lock to avoid deadlock
	for _, endFunc := range endFuncs {
		endFunc()
	}

	return aw.stream.Close()
}

// CloseWithError ends all active announcements and signals an error condition via the given code.
func (aw *AnnouncementWriter) CloseWithError(code AnnounceErrorCode) error {
	aw.mu.Lock()
	// Collect end functions to call after releasing the lock
	var endFuncs []func()
	for _, active := range aw.actives {
		if active.end != nil {
			endFuncs = append(endFuncs, active.end)
		}
	}
	aw.actives = nil
	aw.mu.Unlock()

	// Call end functions without holding the lock to avoid deadlock
	for _, endFunc := range endFuncs {
		endFunc()
	}

	cancelStreamWithError(aw.stream, transport.StreamErrorCode(code))

	return nil
}

// Context returns the AnnouncementWriter's context.
func (aw *AnnouncementWriter) Context() context.Context {
	if aw == nil || aw.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return aw.ctx
}

// activeAnnouncement represents an active announcement being managed by AnnouncementWriter.
type activeAnnouncement struct {
	announcement *Announcement
	end          func() // Function to clean up the activeAnnouncement
}
