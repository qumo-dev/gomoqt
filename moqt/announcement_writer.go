package moqt

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

// newAnnouncementWriter serves the peer's announcement request for prefix.
// excludeHop is the Hop ID the peer declared in its SETUP (0 for none): a path
// through it is never advertised to the peer. scope, when set, is what the
// peer may list: the writer closes with UNAUTHORIZED once prefix lies within
// no root of it.
func newAnnouncementWriter(stream transport.Stream, prefix prefix, localHopID uint64, excludeHop uint64, logger *slog.Logger, scope *authScope) *AnnouncementWriter {
	if !isValidPrefix(prefix) {
		panic("invalid prefix for AnnouncementWriter")
	}

	sas := &AnnouncementWriter{
		prefix:     prefix,
		stream:     stream,
		ctx:        context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeAnnounce),
		actives:    make(map[suffix]*activeAnnouncement),
		initDone:   make(chan struct{}),
		logger:     logger,
		localHopID: localHopID,
		excludeHop: excludeHop,
		scope:      scope,
	}
	if scope != nil {
		context.AfterFunc(sas.ctx, scope.watch(sas))
	}

	return sas
}

// rescope closes the writer with UNAUTHORIZED once its prefix lies within no
// root of the scope.
func (aw *AnnouncementWriter) rescope() {
	aw.mu.RLock()
	closed := aw.closed
	aw.mu.RUnlock()
	// A writer already closed ended its stream; a reset would undo a clean
	// end.
	if closed || aw.ctx.Err() != nil {
		return
	}
	if !aw.scope.canList(aw.prefix) {
		_ = aw.CloseWithError(AnnounceErrorCodeUnauthorized) // not actionable: it only resets the stream
	}
}

// errAnnouncementWriterClosed is the init error of a writer closed before its
// init could take the active announcements.
var errAnnouncementWriterClosed = errors.New("moqt: announcement writer closed")

// AnnouncementWriter manages the sending of announcements for a specified prefix.
// It handles initialization, sending active announcements, and cleanup.
//
// Each route has at most one advertisement on the stream (moq-lite-06
// §5.1.1): the first ANNOUNCE_START for it assigns an Announce ID, a change of
// the announcement behind it travels as ANNOUNCE_UPDATE on that ID, and its end
// as ANNOUNCE_END.
type AnnouncementWriter struct {
	prefix     prefix
	stream     transport.Stream
	ctx        context.Context
	logger     *slog.Logger
	localHopID uint64 // this node's Hop ID, carried in ANNOUNCE_OK
	excludeHop uint64 // the peer's SETUP Hop ID: skip paths that contain it
	scope      *authScope

	mu      sync.RWMutex
	actives map[suffix]*activeAnnouncement
	// nextID is the Announce ID the next ANNOUNCE_START assigns. Both ends
	// count ANNOUNCE_STARTs, so it must advance with every one sent.
	nextID uint64
	// closed is set once Close or CloseWithError has run; the writer then
	// takes no more announcements, even from an init still running.
	closed bool

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

// shouldExclude reports whether ann must not be advertised to the peer: its
// hop path — including this node's own Hop ID, the implicit trailing entry
// ANNOUNCE_OK carries — contains the Hop ID the peer declared (moq-lite-06
// §5.1.1.1). The peer could only discard it, and acting on it would loop.
func (aw *AnnouncementWriter) shouldExclude(ann *Announcement) bool {
	if aw.excludeHop == 0 {
		return false
	}
	if aw.excludeHop == aw.localHopID {
		return true
	}
	return slices.Contains(ann.HopIDs(), aw.excludeHop)
}

// routeSuffix returns the route suffix ann is advertised under on this
// stream: the part of its path past the requested prefix, or "" for a route
// at or above the prefix, which presents clamped to it (moq-lite-06 §5.1.1).
// Matching is per path segment, so a path never matches half a segment.
func (aw *AnnouncementWriter) routeSuffix(ann *Announcement) (suffix, bool) {
	return routeSuffix(ann.BroadcastPath(), aw.prefix)
}

// routeSuffix is AnnouncementWriter.routeSuffix for a path and a prefix.
func routeSuffix(path BroadcastPath, pfx prefix) (suffix, bool) {
	if sfx, ok := path.GetSuffix(pfx); ok {
		return sfx, true
	}
	// A prefix ends in "/", so path covers it when path is the root or path
	// followed by "/" leads the prefix.
	p := string(path)
	if p == "/" || (strings.HasPrefix(pfx, p) && pfx[len(p)] == '/') {
		return "", true
	}
	return "", false
}

// startMessage builds the ANNOUNCE_START for ann under sfx. This node's own
// Hop ID is NOT in the list: ANNOUNCE_OK carries it once as the implicit
// trailing entry.
func startMessage(sfx suffix, ann *Announcement) message.AnnounceStartMessage {
	r := ann.routeInfo()
	return message.AnnounceStartMessage{
		RoutePrefixSuffix: sfx,
		HopIDs:            r.hopIDs,
		WarmRouteCost:     r.warmCost,
		ColdRouteCost:     r.coldCost,
	}
}

// init snapshots the currently active announcements, sends ANNOUNCE_OK and an
// ANNOUNCE_START for each route, and watches each for its end.
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
			sfx, ok := aw.routeSuffix(ann)
			if !ok {
				continue
			}
			if aw.shouldExclude(ann) {
				continue
			}
			if active, ok := actives[sfx]; ok {
				// Several routes clamp to the request prefix: advertise one,
				// keep the rest to fall back on.
				active.standby = append(active.standby, ann)
				continue
			}
			actives[sfx] = &activeAnnouncement{announcement: ann}
		}

		aw.mu.Lock()
		if aw.closed {
			err = errAnnouncementWriterClosed
			aw.initErr = err
			aw.mu.Unlock()
			return
		}
		aw.actives = actives
		aw.mu.Unlock()

		// ANNOUNCE_OK is sent exactly once, before any announcement, carrying
		// this node's Hop ID and the size of the initial set.
		err = message.AnnounceOkMessage{
			HopID:       aw.localHopID,
			ActiveCount: uint64(len(actives)),
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

		for sfx, active := range actives {
			aw.mu.Lock()
			active.id = aw.nextID
			aw.nextID++
			err = startMessage(sfx, active.announcement).Encode(aw.stream)
			if err != nil {
				if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
					err = &AnnounceError{StreamError: strErr}
				}
				aw.initErr = err
				aw.mu.Unlock()
				return
			}
			aw.watchLocked(sfx, active)
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

// watchLocked watches the announcement active advertises, re-advertising or
// ending the route when it ends. Caller MUST hold aw.mu.
func (aw *AnnouncementWriter) watchLocked(sfx suffix, active *activeAnnouncement) {
	if aw.actives[sfx] != active {
		// The writer was closed while the caller had released aw.mu.
		return
	}
	ann := active.announcement
	active.stop = ann.AfterFunc(func() { aw.ended(sfx, active, ann) })
}

// ended handles the end of ann, the announcement behind active. A route with a
// standby still active moves to it with ANNOUNCE_UPDATE; otherwise it ends
// with ANNOUNCE_END. Nothing is sent once the writer has closed or active is no
// longer the route's advertisement. It takes aw.mu.
func (aw *AnnouncementWriter) ended(sfx suffix, active *activeAnnouncement, ann *Announcement) {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	if aw.actives[sfx] != active || active.announcement != ann {
		return
	}

	for len(active.standby) > 0 {
		next := active.standby[len(active.standby)-1]
		active.standby = active.standby[:len(active.standby)-1]
		if !next.IsActive() || aw.shouldExclude(next) {
			continue
		}
		active.announcement = next
		aw.sendUpdateLocked(active)
		aw.watchLocked(sfx, active)
		return
	}

	delete(aw.actives, sfx)
	aw.sendEndLocked(active.id)
}

// sendEndLocked sends ANNOUNCE_END for id. A stream that has already ended,
// as it has once the session closes, is not written to: its end ends every
// announcement on the peer. Caller MUST hold aw.mu.
func (aw *AnnouncementWriter) sendEndLocked(id uint64) {
	if aw.ctx.Err() != nil {
		return
	}
	if err := (message.AnnounceEndMessage{AnnounceID: id}).Encode(aw.stream); err != nil {
		aw.logError("failed to encode ANNOUNCE_END message", err)
	}
}

// sendUpdateLocked sends ANNOUNCE_UPDATE carrying the routing metadata of the
// announcement now behind active. Caller MUST hold aw.mu.
func (aw *AnnouncementWriter) sendUpdateLocked(active *activeAnnouncement) {
	if aw.ctx.Err() != nil {
		return
	}
	r := active.announcement.routeInfo()
	err := message.AnnounceUpdateMessage{
		AnnounceID:    active.id,
		HopIDs:        r.hopIDs,
		WarmRouteCost: r.warmCost,
		ColdRouteCost: r.coldCost,
	}.Encode(aw.stream)
	if err != nil {
		aw.logError("failed to encode ANNOUNCE_UPDATE message", err)
	}
}

// SendAnnouncement advertises announcement if it is active and not already
// advertised. A route already advertised by another announcement moves to
// this one with ANNOUNCE_UPDATE, keeping its Announce ID.
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

	if aw.shouldExclude(announcement) {
		return nil
	}

	sfx, ok := aw.routeSuffix(announcement)
	if !ok {
		return errors.New("moq: broadcast path with invalid prefix")
	}

	aw.mu.Lock()
	defer aw.mu.Unlock()

	// The writer may have been closed; it must not encode onto a closing
	// stream.
	if aw.closed {
		return nil
	}

	if active, exists := aw.actives[sfx]; exists {
		if active.announcement == announcement || slices.Contains(active.standby, announcement) {
			return nil // Already advertised, no need to re-announce
		}
		// The route is advertised already: a second ANNOUNCE_START for it
		// would be a protocol violation. Move it to the newer announcement
		// in place, keeping the older one to fall back on while it lasts.
		if active.stop != nil {
			active.stop()
		}
		if prev := active.announcement; prev.IsActive() {
			active.standby = append(active.standby, prev)
		}
		active.announcement = announcement
		aw.sendUpdateLocked(active)
		aw.watchLocked(sfx, active)
		return nil
	}

	active := &activeAnnouncement{announcement: announcement, id: aw.nextID}
	err := startMessage(sfx, announcement).Encode(aw.stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return &AnnounceError{
				StreamError: strErr,
			}
		}

		return err
	}
	aw.nextID++

	aw.actives[sfx] = active
	aw.watchLocked(sfx, active)

	return nil
}

// Close finishes the stream. The peer then treats every announcement on it as
// ended (moq-lite-06 §5.1.1), so no ANNOUNCE_END is sent.
func (aw *AnnouncementWriter) Close() error {
	aw.stopAll()
	return aw.stream.Close()
}

// CloseWithError resets the stream with the given code, which ends every
// active announcement on the peer as well.
func (aw *AnnouncementWriter) CloseWithError(code AnnounceErrorCode) error {
	aw.stopAll()
	cancelStreamWithError(aw.stream, transport.StreamErrorCode(code))
	return nil
}

// stopAll empties the writer and stops watching the announcements that were
// active, sending nothing.
func (aw *AnnouncementWriter) stopAll() {
	aw.mu.Lock()
	defer aw.mu.Unlock()
	for _, active := range aw.actives {
		if active.stop != nil {
			active.stop()
		}
	}
	aw.actives = nil
	aw.closed = true
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

// activeAnnouncement is one route advertised on the stream.
type activeAnnouncement struct {
	// id is the Announce ID its ANNOUNCE_START assigned.
	id uint64
	// announcement is the announcement the route currently advertises.
	announcement *Announcement
	// standby are other announcements for the same route, newest last: the
	// route moves to one with ANNOUNCE_UPDATE when announcement ends. Only
	// routes clamped to the request prefix collect them.
	standby []*Announcement
	stop    func() bool // stops the watch watchLocked set, if it has not run
}
