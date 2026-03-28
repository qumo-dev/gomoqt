package moqt

import (
	"context"
	"errors"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

// newAnnouncementWriter creates a new AnnouncementWriter for the given stream and prefix.
func newAnnouncementWriter(stream Stream, prefix prefix) *AnnouncementWriter {
	if !isValidPrefix(prefix) {
		panic("invalid prefix for AnnouncementWriter")
	}

	sas := &AnnouncementWriter{
		prefix:  prefix,
		stream:  stream,
		ctx:     context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeAnnounce),
		actives: make(map[suffix]*activeAnnouncement),
		initCh:  make(chan struct{}),
	}

	return sas
}

// AnnouncementWriter manages the sending of announcements for a specified prefix.
// It handles initialization, sending active announcements, and cleanup.
type AnnouncementWriter struct {
	prefix prefix
	stream Stream
	ctx    context.Context

	mu      sync.RWMutex
	actives map[suffix]*activeAnnouncement

	initCh   chan struct{}
	initOnce sync.Once
}

// init initializes the AnnouncementWriter with the given announcements.
// It sends an AnnounceInitMessage and sets up end handlers for active announcements.
func (aw *AnnouncementWriter) init(announcements map[*Announcement]struct{}) error {
	var err error
	aw.initOnce.Do(func() {
		if aw.ctx.Err() != nil {
			err = Cause(aw.ctx)
			return
		}

		actives := make(map[suffix]*activeAnnouncement)
		suffixes := make([]suffix, 0, len(announcements))

		for ann := range announcements {
			if !ann.IsActive() {
				continue
			}
			sfx, ok := ann.BroadcastPath().GetSuffix(aw.prefix)
			if !ok {
				continue
			}
			// Always replace with the latest active announcement for the suffix
			actives[sfx] = &activeAnnouncement{announcement: ann}
			suffixes = append(suffixes, sfx)
		}

		err = message.AnnounceInitMessage{
			Suffixes: suffixes,
		}.Encode(aw.stream)
		if err != nil {
			var strErr *StreamError
			if errors.As(err, &strErr) {
				err = &AnnounceError{StreamError: strErr}
			}
			return
		}

		aw.actives = actives

		// Register end functions for each active announcement
		for sfx, active := range actives {
			aw.registerEndHandler(sfx, active.announcement)
		}
		close(aw.initCh)
	})
	return err
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
			if err := (message.AnnounceMessage{
				AnnounceStatus: message.ENDED,
				TrackSuffix:    sfx,
			}).Encode(aw.stream); err != nil {
				// Silently ignore encode errors
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
			AnnounceStatus: message.ENDED,
			TrackSuffix:    sfx,
		}).Encode(aw.stream); err != nil {
			// Silently ignore encode errors
		}
	}
}

// SendAnnouncement sends an announcement if it's active and not already sent.
// It replaces any existing announcement for the same suffix.
func (aw *AnnouncementWriter) SendAnnouncement(announcement *Announcement) error {
	// Wait for initialization to complete
	select {
	case <-aw.initCh:
		// Initialization complete
	case <-aw.ctx.Done():
		return Cause(aw.ctx)
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
		AnnounceStatus: message.ACTIVE,
		TrackSuffix:    suffix,
	}.Encode(aw.stream)
	if err != nil {
		var strErr *StreamError
		if errors.As(err, &strErr) {
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

	strErrCode := StreamErrorCode(code)
	aw.stream.CancelWrite(strErrCode)
	aw.stream.CancelRead(strErrCode)

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
