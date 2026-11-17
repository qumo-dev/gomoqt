package moqt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

// announceReaderConfig carries the session state an AnnouncementReader needs
// to apply moq-lite-06's receiver rules. The zero value applies none of them
// beyond the stream's own: no loop check, the default link cost, and a
// violation only ends the reader.
type announceReaderConfig struct {
	// localHopID is this endpoint's Hop ID; an announcement whose path
	// contains it has looped back and is discarded. 0 disables the check.
	localHopID uint64
	// peerCost returns the Cost the peer declared in its SETUP, added to both
	// route costs of every announcement it sends. It may wait for the peer's
	// SETUP; it is called once, at the first announcement. Nil means
	// message.DefaultCost.
	peerCost func() uint64
	// onProtocolViolation closes the session: moq-lite-06 makes every Announce
	// Stream violation fatal to it. Nil only ends the reader.
	onProtocolViolation func(msg string)
}

// newAnnouncementReader reads the peer's announcements under prefix. scope,
// when set, is what the peer may publish: an announcement outside it is
// dropped.
func newAnnouncementReader(stream transport.Stream, prefix prefix, scope *authScope, cfg announceReaderConfig) *AnnouncementReader {
	if !isValidPrefix(prefix) {
		panic("invalid prefix for AnnouncementReader")
	}

	readerCtx, cancelCause := context.WithCancelCause(stream.Context())

	ar := &AnnouncementReader{
		ctx:         context.WithValue(readerCtx, biStreamTypeCtxKey, message.StreamTypeAnnounce),
		stream:      stream,
		prefix:      prefix,
		routes:      make(map[uint64]suffix),
		advertised:  make(map[suffix]*advertisedRoute),
		pendings:    make([]*Announcement, 0),
		announcedCh: make(chan struct{}, 1),
		cancelCause: cancelCause,
		scope:       scope,
		cfg:         cfg,
	}

	if scope != nil {
		// Watching from before the first read, no scope change misses it.
		context.AfterFunc(ar.ctx, scope.watch(ar))
	}

	go ar.run()

	return ar
}

// AnnouncementReader receives and manages broadcast announcements for a prefix from
// a remote peer. It maintains active announcements and notifies the caller when new
// announcements arrive or existing ones end.
type AnnouncementReader struct {
	stream transport.Stream
	prefix prefix

	ctx context.Context

	cfg announceReaderConfig

	announcementsMu sync.Mutex

	// nextID is the Announce ID the next ANNOUNCE_START assigns.
	nextID uint64
	// routes maps each live Announce ID to the route suffix it advertises.
	routes map[uint64]suffix
	// advertised holds the peer's current advertisement for each route
	// suffix. Each suffix has at most one per stream.
	advertised map[suffix]*advertisedRoute

	pendings    []*Announcement
	announcedCh chan struct{} // notify when new announcement is available

	// scope, when set, is what the peer may publish; a broadcast outside it is
	// dropped.
	scope *authScope

	// cancelCause cancels readerCtx (and thus ar.ctx) when the announce
	// goroutine hits a fatal decode error, so ReceiveAnnouncement unblocks.
	cancelCause context.CancelCauseFunc
}

// advertisedRoute is the peer's current advertisement of one route on the
// stream.
type advertisedRoute struct {
	// route is its latest routing metadata, kept while it is discarded so a
	// scope that widens again can bring it back.
	route announceRoute
	// ann is the announcement handed to the application, or nil while the
	// advertisement is discarded: it loops back through this endpoint, or
	// lies outside the scope. The peer still advertises it, so its Announce
	// ID stays live, and an ANNOUNCE_UPDATE or a wider scope may bring it
	// back.
	ann *Announcement
}

// errAnnounceViolation marks an Announce Stream message that breaks
// moq-lite-06 §5.1.1, which closes the session.
var errAnnounceViolation = errors.New("moqt: announce stream protocol violation")

// run reads ANNOUNCE_OK and then the announcements, until the stream ends or
// breaks a rule.
func (ar *AnnouncementReader) run() {
	// The publisher sends ANNOUNCE_OK exactly once before any announcement.
	// Its Hop ID is the implicit trailing entry of every announcement's Hop
	// ID list on this stream. An announcement in its place does not parse as
	// one, which is what makes an announcement before ANNOUNCE_OK a
	// violation.
	r := &countingReader{r: ar.stream}
	var okMsg message.AnnounceOkMessage
	if err := okMsg.Decode(r); err != nil {
		ar.end(decodeErr(r, "ANNOUNCE_OK", err))
		return
	}
	peerHopID := okMsg.HopID

	// The peer's SETUP Cost is looked up at the first announcement, not
	// here: SETUP arrives on its own stream, and waiting for it before an
	// empty stream ends would hold the reader for the SETUP timeout.
	peerCost := sync.OnceValue(func() uint64 {
		if ar.cfg.peerCost == nil {
			return message.DefaultCost
		}
		return ar.cfg.peerCost()
	})

	for {
		r.n = 0
		msgType, err := message.ReadMessageType(r)
		if err != nil {
			ar.end(decodeErr(r, "announcement type", err))
			return
		}

		switch msgType {
		case message.MessageTypeAnnounceStart:
			var am message.AnnounceStartMessage
			if err := am.Decode(r); err != nil {
				ar.end(decodeErr(r, "ANNOUNCE_START", err))
				return
			}
			route, err := receivedRoute(am.HopIDs, peerHopID, am.WarmRouteCost, am.ColdRouteCost, peerCost())
			if err == nil {
				err = ar.start(am.RoutePrefixSuffix, route)
			}
			if err != nil {
				ar.end(err)
				return
			}
		case message.MessageTypeAnnounceEnd:
			var am message.AnnounceEndMessage
			if err := am.Decode(r); err != nil {
				ar.end(decodeErr(r, "ANNOUNCE_END", err))
				return
			}
			if err := ar.retract(am.AnnounceID); err != nil {
				ar.end(err)
				return
			}
		case message.MessageTypeAnnounceUpdate:
			var am message.AnnounceUpdateMessage
			if err := am.Decode(r); err != nil {
				ar.end(decodeErr(r, "ANNOUNCE_UPDATE", err))
				return
			}
			route, err := receivedRoute(am.HopIDs, peerHopID, am.WarmRouteCost, am.ColdRouteCost, peerCost())
			if err == nil {
				err = ar.update(am.AnnounceID, route)
			}
			if err != nil {
				ar.end(err)
				return
			}
		default:
			ar.end(fmt.Errorf("%w: unknown announcement type %d", errAnnounceViolation, msgType))
			return
		}
	}
}

// errDuplicateHopID reports a Hop ID list that names one non-zero Hop ID
// twice, a protocol violation (moq-lite-06 §7.6). Repeated zeros are legal:
// 0 identifies nothing.
var errDuplicateHopID = errors.New("duplicate non-zero hop id")

// checkHopIDs returns errDuplicateHopID when a non-zero Hop ID appears twice
// in ids. A real hop path is short, and a quadratic scan of it allocates
// nothing; the list comes from the peer, though, and one message can carry
// thousands of entries, so a long one is checked with a set instead.
func checkHopIDs(ids []uint64) error {
	if len(ids) > shortHopList {
		seen := make(map[uint64]struct{}, len(ids))
		for _, id := range ids {
			if id == 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				return errDuplicateHopID
			}
			seen[id] = struct{}{}
		}
		return nil
	}
	for i, id := range ids {
		if id == 0 {
			continue
		}
		if slices.Contains(ids[i+1:], id) {
			return errDuplicateHopID
		}
	}
	return nil
}

// shortHopList is the longest Hop ID list checkHopIDs scans quadratically.
const shortHopList = 32

// receivedRoute reconstructs an advertisement's routing metadata: its Hop ID
// list plus the peer's implicit ANNOUNCE_OK Hop ID, and both route costs plus
// the cost the peer declared for its link, saturating at the varint ceiling.
func receivedRoute(hopIDs []uint64, peerHopID, warm, cold, peerCost uint64) (announceRoute, error) {
	if err := checkHopIDs(hopIDs); err != nil {
		return announceRoute{}, fmt.Errorf("%w: %w", errAnnounceViolation, err)
	}
	return announceRoute{
		hopIDs:   append(slices.Clip(hopIDs), peerHopID),
		warmCost: addCost(warm, peerCost),
		coldCost: addCost(cold, peerCost),
	}, nil
}

// maxRouteCost is the saturation ceiling of a route cost: the largest value a
// varint carries, so a forwarded sum can always be encoded again.
const maxRouteCost = 1<<62 - 1

// addCost adds two route costs, saturating at maxRouteCost.
func addCost(a, b uint64) uint64 {
	if a > maxRouteCost-min(b, maxRouteCost) {
		return maxRouteCost
	}
	return a + b
}

// loops reports whether a hop path runs through this endpoint. A Hop ID of 0
// identifies nothing, so an endpoint without one never detects a loop.
func (ar *AnnouncementReader) loops(r announceRoute) bool {
	return ar.cfg.localHopID != 0 && slices.Contains(r.hopIDs, ar.cfg.localHopID)
}

// start handles ANNOUNCE_START: it assigns the next Announce ID and, unless the
// route loops or lies outside the scope, announces it to the application.
func (ar *AnnouncementReader) start(sfx suffix, route announceRoute) error {
	ar.announcementsMu.Lock()
	defer ar.announcementsMu.Unlock()

	id := ar.nextID
	ar.nextID++

	if _, ok := ar.advertised[sfx]; ok {
		return fmt.Errorf("%w: ANNOUNCE_START for an already advertised route %q", errAnnounceViolation, sfx)
	}
	ar.routes[id] = sfx
	adv := &advertisedRoute{route: route}
	if ar.admits(sfx, route) {
		adv.ann = ar.activateLocked(sfx, route)
	}
	ar.advertised[sfx] = adv
	return nil
}

// retract handles ANNOUNCE_END: it retires the Announce ID and ends the
// announcement it carried.
func (ar *AnnouncementReader) retract(id uint64) error {
	ar.announcementsMu.Lock()
	defer ar.announcementsMu.Unlock()

	sfx, ok := ar.routes[id]
	if !ok {
		return fmt.Errorf("%w: ANNOUNCE_END for unknown or retired Announce ID %d", errAnnounceViolation, id)
	}
	delete(ar.routes, id)
	adv := ar.advertised[sfx]
	delete(ar.advertised, sfx)
	if adv.ann != nil {
		adv.ann.end()
	}
	return nil
}

// update handles ANNOUNCE_UPDATE: it replaces the route's metadata in place,
// leaving the announcement, and so the subscriptions under it, undisturbed. An
// update that makes the route loop discards it; one that stops it looping
// announces it again.
func (ar *AnnouncementReader) update(id uint64, route announceRoute) error {
	ar.announcementsMu.Lock()
	defer ar.announcementsMu.Unlock()

	sfx, ok := ar.routes[id]
	if !ok {
		return fmt.Errorf("%w: ANNOUNCE_UPDATE for unknown or retired Announce ID %d", errAnnounceViolation, id)
	}
	adv := ar.advertised[sfx]
	adv.route = route

	switch admitted := ar.admits(sfx, route); {
	case admitted && adv.ann != nil:
		adv.ann.setRoute(route)
	case admitted:
		adv.ann = ar.activateLocked(sfx, route)
	case adv.ann != nil:
		adv.ann.end()
		adv.ann = nil
	}
	return nil
}

// admits reports whether an advertisement is handed to the application: it
// must not loop back through this endpoint (moq-lite-06 §5.1.1.1), and the
// peer must be allowed to publish it.
func (ar *AnnouncementReader) admits(sfx suffix, route announceRoute) bool {
	if ar.loops(route) {
		return false
	}
	return ar.scope == nil || ar.scope.canPublish(BroadcastPath(ar.prefix+sfx))
}

// end stops reading for cause. A rule broken on the stream closes the
// session (moq-lite-06 §5.1.1); a stream the peer ended or reset only ends
// the reader. Either way every announcement on it ends, as they are children
// of the reader's context.
func (ar *AnnouncementReader) end(cause error) {
	if errors.Is(cause, errAnnounceViolation) && ar.cfg.onProtocolViolation != nil {
		ar.cfg.onProtocolViolation(cause.Error())
	}
	ar.fail(cause)
}

// decodeErr classifies a failed read of what. Once part of a message has been
// read, a failure that is not the transport's is a message that does not
// parse: an unexpected length, a Hop Count that disagrees with its entries, a
// message cut short by FIN, or an announcement where ANNOUNCE_OK belongs. Those
// are violations; the stream ending between messages, being reset, or the
// session going away are not.
func decodeErr(r *countingReader, what string, err error) error {
	if r.n == 0 {
		return err
	}
	if _, ok := errors.AsType[*transport.StreamError](err); ok {
		return err
	}
	if _, ok := errors.AsType[*transport.ApplicationError](err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: malformed %s: %w", errAnnounceViolation, what, err)
}

// countingReader counts the bytes read through it since n was last reset.
type countingReader struct {
	r io.Reader
	n int
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += n
	return n, err
}

// activateLocked makes a new active announcement for suffix and queues it for
// ReceiveAnnouncement. Caller MUST hold announcementsMu.
func (ar *AnnouncementReader) activateLocked(sfx suffix, route announceRoute) *Announcement {
	ann, _ := NewAnnouncement(ar.ctx, BroadcastPath(ar.prefix+sfx))
	ann.setRoute(route)
	ar.limitLocked(ann)
	ar.pendings = append(ar.pendings, ann)
	select {
	case ar.announcedCh <- struct{}{}:
	default:
	}
	return ann
}

// limitLocked makes ann exact-only when the scope lets the peer publish its
// path but not every path beneath it, which an announcement claims as a
// route. Caller MUST hold announcementsMu.
func (ar *AnnouncementReader) limitLocked(ann *Announcement) {
	ann.exactOnly.Store(ar.scope != nil && !ar.scope.canPublishBeneath(ann.BroadcastPath()))
}

// rescope re-applies the scope after it changed: it ends each active
// announcement the peer may no longer publish, and announces again each route
// the peer still advertises that the scope now allows. A route that loops
// stays discarded.
func (ar *AnnouncementReader) rescope() {
	if ar.scope == nil {
		return
	}

	ar.announcementsMu.Lock()
	defer ar.announcementsMu.Unlock()
	if ar.ctx.Err() != nil {
		return
	}

	for sfx, adv := range ar.advertised {
		switch admitted := ar.admits(sfx, adv.route); {
		case admitted && adv.ann == nil:
			adv.ann = ar.activateLocked(sfx, adv.route)
		case !admitted && adv.ann != nil:
			adv.ann.end()
			adv.ann = nil
		case admitted:
			ar.limitLocked(adv.ann)
		}
	}
}

// fail cancels the reader context with cause, unblocking ReceiveAnnouncement.
func (ar *AnnouncementReader) fail(err error) {
	if ar.cancelCause != nil {
		ar.cancelCause(err)
	}
}

// ReceiveAnnouncement blocks until an announcement for the configured prefix is available or until ctx or the reader's context is canceled.
// It returns the next pending Announcement or an error describing the cancellation cause.
// Each route is returned when it becomes active; its ending is not returned
// again but observed on the Announcement (Done, IsActive, AfterFunc). An
// ANNOUNCE_UPDATE changes the Announcement in place (see HopIDs) and returns
// nothing new.
//
// An announced path is a route: a prefix under which the peer claims it can
// serve, not a promise that a broadcast exists there (moq-lite-06 §5.1.1). By
// convention a publisher announces each broadcast's exact path. A route the
// peer holds above the requested prefix arrives clamped to it, as the prefix
// itself.
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
// It yields each route when it becomes active, as ReceiveAnnouncement
// returns it; an ending is observed on the Announcement itself.
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

	// Cancel the reader context so any blocked ReceiveAnnouncement unblocks
	// promptly (rather than busy-spinning on a closed announcedCh).
	ras.fail(context.Canceled)

	if ras.announcedCh != nil {
		close(ras.announcedCh)
		ras.announcedCh = nil
	}

	return ras.stream.Close()
}

// CloseWithError closes the AnnouncementReader with an error, ending all
// active announcements and canceling the stream with the specified error
// code.
func (ras *AnnouncementReader) CloseWithError(code AnnounceErrorCode) {
	ras.announcementsMu.Lock()
	defer ras.announcementsMu.Unlock()

	ras.fail(context.Canceled)

	if ras.announcedCh != nil {
		close(ras.announcedCh)
		ras.announcedCh = nil
	}

	cancelStreamWithError(ras.stream, transport.StreamErrorCode(code))
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
