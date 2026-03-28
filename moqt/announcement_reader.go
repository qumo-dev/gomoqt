package moqt

import (
	"context"
	"iter"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

func newAnnouncementReader(stream Stream, prefix prefix, initSuffixes []suffix) *AnnouncementReader {
	if !isValidPrefix(prefix) {
		panic("invalid prefix for AnnouncementReader")
	}

	ar := &AnnouncementReader{
		ctx:         context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeAnnounce),
		stream:      stream,
		prefix:      prefix,
		actives:     make(map[suffix]*Announcement),
		pendings:    make([]*Announcement, 0),
		announcedCh: make(chan struct{}, 1),
	}

	for _, suffix := range initSuffixes {
		ann, _ := NewAnnouncement(stream.Context(), BroadcastPath(prefix+suffix))
		ar.actives[suffix] = ann
		ar.pendings = append(ar.pendings, ann)
	}

	// Receive announcements in a separate goroutine
	go func() {
		var am message.AnnounceMessage
		var err error

		for {
			err = am.Decode(ar.stream)
			if err != nil {
				return
			}

			// Use an inner scope to ensure Lock/Unlock are paired even across continue/return
			switch am.AnnounceStatus {
			case message.ACTIVE:
				{
					suffix := am.TrackSuffix
					var shouldClose bool
					// Mutate maps under lock
					func() {
						ar.announcementsMu.Lock()
						defer func() {
							ar.announcementsMu.Unlock()
						}()
						old, ok := ar.actives[suffix]
						if !ok || !old.IsActive() {
							ann, _ := NewAnnouncement(ar.ctx, BroadcastPath(ar.prefix+suffix))
							ar.actives[suffix] = ann
							ar.pendings = append(ar.pendings, ann)
							select {
							case ar.announcedCh <- struct{}{}:
							default:
							}
							return
						}
						shouldClose = true
					}()

					if shouldClose {
						_ = ar.CloseWithError(DuplicatedAnnounceErrorCode)
						return
					}
				}
			case message.ENDED:
				{
					suffix := am.TrackSuffix
					var handled bool
					func() {
						ar.announcementsMu.Lock()
						defer func() {
							ar.announcementsMu.Unlock()
						}()
						old, ok := ar.actives[suffix]
						if ok && old.IsActive() {
							old.end()
							delete(ar.actives, suffix)
							handled = true
						}
					}()
					if handled {
						continue
					}
					_ = ar.CloseWithError(DuplicatedAnnounceErrorCode)
					return
				}
			default:
				_ = ar.CloseWithError(InvalidAnnounceStatusErrorCode)
				return
			}
		}
	}()

	return ar
}

// AnnouncementReader receives and manages broadcast announcements from a remote peer.
// It maintains a list of active announcements and notifies when new announcements
// are received or existing ones are canceled.
type AnnouncementReader struct {
	stream Stream
	prefix prefix

	ctx context.Context

	// Track Suffix -> Announcement
	announcementsMu sync.Mutex

	actives map[suffix]*Announcement

	pendings    []*Announcement
	announcedCh chan struct{} // notify when new announcement is available
}

// ReceiveAnnouncement blocks until an announcement for the configured prefix is available or until ctx or the reader's context is canceled.
// It returns the next pending Announcement or an error describing the cancellation cause.
func (ras *AnnouncementReader) ReceiveAnnouncement(ctx context.Context) (*Announcement, error) {
	for {
		ras.announcementsMu.Lock()

		if len(ras.pendings) > 0 {
			next := ras.pendings[0]
			ras.pendings = ras.pendings[1:]

			ras.announcementsMu.Unlock()

			return next, nil
		}

		if ras.ctx.Err() != nil {
			ras.announcementsMu.Unlock()
			return nil, Cause(ras.ctx)
		}

		announceCh := ras.announcedCh

		ras.announcementsMu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ras.ctx.Done():
			return nil, Cause(ras.ctx)
		case <-announceCh:
			// New announcement available, loop to check pendings
			continue
		}
	}
}

// Announcements returns an iterator that yields Announcement values for the configured prefix until ctx or the reader's context is canceled.
func (ras *AnnouncementReader) Announcements(ctx context.Context) iter.Seq[*Announcement] {
	return func(yield func(*Announcement) bool) {
		for {
			ann, err := ras.ReceiveAnnouncement(ctx)
			if err != nil {
				return
			}

			if !yield(ann) {
				return
			}
		}
	}

}

// Close closes the AnnouncementReader and releases resources. It is safe to call multiple times.
func (ras *AnnouncementReader) Close() error {
	ras.announcementsMu.Lock()
	defer ras.announcementsMu.Unlock()

	if ras.ctx.Err() != nil {
		return nil
	}

	if ras.announcedCh != nil {
		close(ras.announcedCh)
		ras.announcedCh = nil
	}

	return ras.stream.Close()
}

// CloseWithError closes the AnnouncementReader with an error, ending all
// active announcements and canceling the stream with the specified error
// code.
func (ras *AnnouncementReader) CloseWithError(code AnnounceErrorCode) error {
	ras.announcementsMu.Lock()
	defer ras.announcementsMu.Unlock()

	if ras.announcedCh != nil {
		close(ras.announcedCh)
		ras.announcedCh = nil
	}

	strErrCode := StreamErrorCode(code)
	ras.stream.CancelRead(strErrCode)
	ras.stream.CancelWrite(strErrCode)

	return nil
}

// Context returns the AnnouncementReader's context. It is canceled when the reader is closed.
func (ras *AnnouncementReader) Context() context.Context {
	if ras == nil || ras.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return ras.ctx
}

// Alias types for better readability
type suffix = string
type prefix = string
