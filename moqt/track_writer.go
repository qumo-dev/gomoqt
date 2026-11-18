package moqt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

type groupWriterManager struct {
	mu           sync.Mutex
	activeGroups map[*GroupWriter]struct{}

	closed bool
}

func newGroupWriterManager() *groupWriterManager {
	return &groupWriterManager{
		activeGroups: make(map[*GroupWriter]struct{}),
	}
}

func (m *groupWriterManager) addGroup(group *GroupWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.activeGroups[group] = struct{}{}
}

func (m *groupWriterManager) removeGroup(group *GroupWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.activeGroups, group)
}

func (m *groupWriterManager) countGroups() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.activeGroups)
}

func (m *groupWriterManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.activeGroups = nil
}

func newTrackWriter(
	broadcastPath BroadcastPath,
	trackName TrackName,
	subscribeStream *receiveSubscribeStream,
	openUniStreamFunc func(context.Context) (transport.SendStream, error),
	onCloseTrackFunc func(),
) *TrackWriter {
	streamCtx := subscribeStream.stream.Context()

	track := &TrackWriter{
		BroadcastPath:     broadcastPath,
		TrackName:         trackName,
		subscribeStream:   subscribeStream,
		groupManager:      newGroupWriterManager(),
		openUniStreamFunc: openUniStreamFunc,
		onCloseTrackFunc:  onCloseTrackFunc,
		ctx:               context.WithValue(streamCtx, biStreamTypeCtxKey, message.StreamTypeSubscribe),
	}
	track.ages = &groupAges{config: track.TrackConfig, timescale: DefaultTimescale}

	return track
}

// TrackWriter writes groups for a published track.
// It manages the lifecycle of active groups for that track.
// The TrackWriter provides output-side methods to send track data.
type TrackWriter struct {
	// BroadcastPath is the path of the broadcast this track belongs to.
	// The value is set when the subscription is accepted and does not change.
	BroadcastPath BroadcastPath

	// TrackName is the name of the track within the broadcast.
	// The value is set when the subscription is accepted and does not change.
	TrackName TrackName

	// pathValues holds what the wildcards of the TrackMux pattern serving
	// the track matched.
	pathValues map[string]string

	subscribeStream *receiveSubscribeStream

	groupManager *groupWriterManager

	mu sync.RWMutex

	// groupSequence is atomically incremented for each OpenGroup call
	groupSequence atomic.Uint64
	// usedZero is set once group 0 has been opened, which OpenGroupAt alone
	// can do without advancing groupSequence.
	usedZero atomic.Bool

	// ages expires the groups the subscriber's Max Age leaves behind.
	ages *groupAges

	openUniStreamFunc func(context.Context) (transport.SendStream, error)

	onCloseTrackFunc func()

	// redirectAllowed reports whether a redirect to uri may be sent to this
	// subscriber, waiting for its SETUP until ctx ends; nil means none may.
	redirectAllowed func(ctx context.Context, uri string) error

	ctx context.Context
}

// PathValue returns what the named wildcard of the TrackMux pattern that last
// routed the subscription matched, like http.Request.PathValue: for a handler
// registered for "/room/{id}/", a subscription to "/room/1/alice" gives "1"
// for "id". It records the routing, not the current path: rewriting
// BroadcastPath leaves it as it was, and handing the TrackWriter to a
// TrackMux routes it again and sets it anew. It returns "" if there is no such
// wildcard, or an announced path served the subscription.
func (w *TrackWriter) PathValue(name string) string {
	return w.pathValues[name]
}

// Close stops publishing and cancels active groups.
// It sends SUBSCRIBE_END with the sequence after the last one used before
// closing the subscribe stream, signaling that no further groups will be
// produced.
func (w *TrackWriter) Close() error {
	// Take the write lock to ensure Close is exclusive with OpenGroup calls.
	// This prevents OpenGroup from running concurrently with Close and
	// provides a deterministic semantics: either the OpenGroup completes
	// entirely before Close proceeds, or Close waits for OpenGroup to finish.
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stopLocked(func(g *GroupWriter) {
		_ = g.Close() // not actionable: the track is ending; the peer sees the group end either way
	})

	if w.subscribeStream == nil {
		return nil
	}

	// Signal that no group at or after the one following the last sequence
	// used will be produced: SUBSCRIBE_END's Group is exclusive. When no
	// group was used it is 0, and without a preceding SUBSCRIBE_OK it says
	// the track ended with no matching groups.
	_ = w.subscribeStream.writeEnd(w.endGroup())

	return w.subscribeStream.close()
}

// endGroup returns the exclusive end SUBSCRIBE_END carries: the sequence
// after the last one opened, skipped or dropped, or 0 when none was.
func (w *TrackWriter) endGroup() GroupSequence {
	last := w.groupSequence.Load()
	if last == 0 && !w.usedZero.Load() {
		return 0
	}
	end := GroupSequence(last + 1)
	// A sequence past EndGroup was refused with ErrGroupOutOfRange and never
	// sent. Naming it would leave the subscriber waiting for groups that
	// cannot arrive, so the range ends right after EndGroup.
	if eg := w.TrackConfig().EndGroup; eg != MinGroupSequence && end > eg+1 {
		end = eg + 1
	}
	return end
}

// stopLocked ends the writer's open groups with endGroup and unregisters the
// track, so no group opens after it. Close, CloseWithError and Redirect share
// it. The caller holds w.mu.
func (w *TrackWriter) stopLocked(endGroup func(*GroupWriter)) {
	if w.groupManager != nil {
		groupManager := w.groupManager
		w.groupManager = nil

		activeGroups := groupManager.activeGroups

		groupManager.close()

		for g := range activeGroups {
			endGroup(g)
		}
	}

	if w.onCloseTrackFunc != nil {
		w.onCloseTrackFunc()
		w.onCloseTrackFunc = nil
	}
}

// Redirect answers the subscription with SUBSCRIBE_REDIRECT instead of
// serving it, sending the subscriber to to, and ends the subscription. It
// must come before the first group: once SUBSCRIBE_OK went out, it fails.
// After it, OpenGroup fails and Close sends nothing.
//
// It returns ErrRedirectNotSupported, and leaves the subscription as it was,
// when the subscriber did not advertise that it follows redirects, and
// ErrClientRedirectURI when a client sets to.URI. It may wait for the
// subscriber's SETUP, which says whether it follows redirects, until ctx ends.
func (w *TrackWriter) Redirect(ctx context.Context, to Redirect) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if to.TrackName == "" {
		to.TrackName = w.TrackName
	}
	if err := to.validate(); err != nil {
		return err
	}
	if w.redirectAllowed == nil {
		return ErrRedirectNotSupported
	}
	if err := w.redirectAllowed(ctx, to.URI); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.subscribeStream == nil {
		return errSubscriptionAnswered
	}
	err := w.subscribeStream.writeRedirect(to.message())
	if errors.Is(err, errSubscriptionAnswered) {
		// Nothing was sent: the subscription is as it was.
		return err
	}
	// Sent, or reset by a failed write: either way the subscription is over.
	// No group is open, as one would have sent SUBSCRIBE_OK and the redirect
	// would have been refused above. A later Close or CloseWithError sends
	// nothing, as the stream knows it was redirected.
	w.stopLocked(func(*GroupWriter) {})
	return err
}

// CloseWithError stops publishing due to an error and cancels active groups.
func (w *TrackWriter) CloseWithError(code SubscribeErrorCode) {
	// Ensure CloseWithError is exclusive with OpenGroup.
	w.mu.Lock()
	defer w.mu.Unlock()

	w.stopLocked(func(g *GroupWriter) {
		g.CancelWrite(InternalGroupErrorCode)
	})

	if w.subscribeStream != nil {
		w.subscribeStream.closeWithError(code)
	}
}

// OpenGroup opens a new group with an automatically incremented sequence number
// and returns a GroupWriter to write frames into it.
// The sequence starts at 1 and increments by 1 for each call.
//
// A sequence below the subscription's StartGroup, or above its EndGroup, is
// refused with ErrGroupOutOfRange, and nothing is sent for it. When the group
// is the start or end group of a peer subscription bounded by frame
// (moq-lite-06 §3.6), the writer sends only the frames it asked for.
//
// The group is opened on a fresh unidirectional QUIC stream. ctx controls
// cancellation and backpressure: when the peer's concurrent-stream limit
// (MAX_STREAMS) is reached, OpenGroup blocks until the peer grants a stream or
// ctx is done, instead of failing. Pass w.Context() to bound the open to the
// track's lifetime, or a deadline-bearing context to drop a group under
// pressure rather than wait.
func (w *TrackWriter) OpenGroup(ctx context.Context) (*GroupWriter, error) {
	seq := GroupSequence(w.groupSequence.Add(1))
	return w.openGroupWithSequence(ctx, seq)
}

// OpenGroupAt opens a new group with the specified sequence number.
// It advances the internal counter to at least seq, so the next OpenGroup
// opens seq+1 and Close's SUBSCRIBE_END ends the range right after seq
// rather than after a group that was never opened.
//
// ctx controls cancellation and backpressure the same way as OpenGroup.
func (w *TrackWriter) OpenGroupAt(ctx context.Context, seq GroupSequence) (*GroupWriter, error) {
	if seq == 0 {
		w.usedZero.Store(true)
	}
	// The counter holds the last sequence used: OpenGroup adds 1 and uses
	// the result. CAS loop ensures correctness under concurrency.
	for {
		cur := w.groupSequence.Load()
		next := max(cur, uint64(seq))
		if next == cur {
			break
		}
		if w.groupSequence.CompareAndSwap(cur, next) {
			break
		}
	}
	return w.openGroupWithSequence(ctx, seq)
}

// SkipGroups skips the next n group sequences without opening them.
// This is useful when you need to intentionally create gaps in the sequence,
// for example, when dropping groups due to packet loss or priority decisions.
func (w *TrackWriter) SkipGroups(n uint64) {
	w.groupSequence.Add(n)
}

func (w *TrackWriter) Context() context.Context {
	return w.ctx
}

// DropGroups sends a SUBSCRIBE_DROP message for an explicit inclusive range.
// The range is expressed in absolute group sequence numbers.
func (w *TrackWriter) DropGroups(drop SubscribeDrop) error {
	if drop.StartGroup == MinGroupSequence || drop.EndGroup == MinGroupSequence {
		return fmt.Errorf("invalid drop range: start=%d end=%d", drop.StartGroup, drop.EndGroup)
	}
	if drop.StartGroup > drop.EndGroup {
		return fmt.Errorf("invalid drop range: start=%d end=%d", drop.StartGroup, drop.EndGroup)
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.Context().Err() != nil {
		return Cause(w.Context())
	}

	if w.subscribeStream == nil {
		return fmt.Errorf("track writer is closed")
	}

	return w.subscribeStream.writeDrop(drop)
}

// DropNextGroups skips the next n groups and emits a SUBSCRIBE_DROP for the
// skipped inclusive range.
func (w *TrackWriter) DropNextGroups(n uint64, code SubscribeErrorCode) error {
	if n == 0 {
		return nil
	}

	for {
		cur := w.groupSequence.Load()
		start := GroupSequence(cur + 1)
		end := GroupSequence(cur + n)

		if end < start {
			return fmt.Errorf("drop range overflow: start=%d end=%d", start, end)
		}

		if w.groupSequence.CompareAndSwap(cur, uint64(end)) {
			return w.DropGroups(SubscribeDrop{
				StartGroup: start,
				EndGroup:   end,
				ErrorCode:  code,
			})
		}
	}
}

// TrackConfig returns the subscriber's current config. It reflects the latest
// SUBSCRIBE_UPDATE only if the publisher is draining updates via ReadUpdate:
// the config advances when ReadUpdate applies an update, not on its own. A
// publisher that never calls ReadUpdate sees the initial (SUBSCRIBE-time)
// config.
func (w *TrackWriter) TrackConfig() *SubscribeConfig {
	if w.subscribeStream == nil {
		return &SubscribeConfig{}
	}

	return w.subscribeStream.TrackConfig()
}

// ReadUpdate blocks until the subscriber sends the next SUBSCRIBE_UPDATE,
// applies it as the track's current config (observable via TrackConfig), and
// returns that config. It returns an error once the subscribe stream ends or is
// closed — the terminal signal for a read loop.
//
// A publisher that wants to follow subscriber updates calls it in a loop from a
// single goroutine; it is the only thing that keeps TrackConfig fresh.
// Publishers that ignore updates simply never call it and spend no goroutine on
// them. Concurrent callers are serialized (their reads cannot interleave) but
// each receives a distinct update in an unspecified order — prefer a single
// caller.
func (w *TrackWriter) ReadUpdate() (*SubscribeConfig, error) {
	if w.subscribeStream == nil {
		return nil, ErrClosedSession
	}
	return w.subscribeStream.readUpdate()
}

// openGroupWithSequence is the internal implementation for opening a group with a specific sequence.
func (w *TrackWriter) openGroupWithSequence(ctx context.Context, seq GroupSequence) (*GroupWriter, error) {
	// Avoid accessing s.ctx directly; it can be nil if the receiveSubscribeStream
	// has been cleared during Close(). Instead, capture the receiveSubscribeStream
	// under lock and validate its context below.

	// Prevent opening a new group if the track has been closed. Capture
	// receiveSubscribeStream under lock so it cannot be set to nil by Close()
	// Acquire a shared lock so multiple OpenGroup calls can proceed
	// concurrently while ensuring Close waits for them to finish.
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Check the context on the captured receiveSubscribeStream instead of s.ctx
	// to avoid nil deref if the embedded field has been cleared by Close().
	if w.Context().Err() != nil {
		return nil, Cause(w.Context())
	}
	if w.groupManager == nil {
		return nil, errTrackWriterStopped
	}

	// Group Start is a floor the publisher MUST NOT deliver below
	// (moq-lite-06 §7.9), and Group End the last group to deliver.
	config := w.TrackConfig()
	if seq < config.StartGroup || (config.EndGroup != MinGroupSequence && seq > config.EndGroup) {
		return nil, ErrGroupOutOfRange
	}
	// A partial group goes only to a subscriber that asked for one
	// (moq-lite-06 §3.6): the start group from its Frame Start, the end group
	// up to its Frame End, every other group whole.
	var frameStart, frameEnd uint64
	if seq == config.StartGroup {
		frameStart = config.startFrame
	}
	if config.EndGroup != MinGroupSequence && seq == config.EndGroup {
		frameEnd = config.endFrame
	}

	// Ensure the first SUBSCRIBE_OK has been sent before opening a group.
	// The first opened group resolves the subscription's absolute start.
	err := w.subscribeStream.ensureOk(seq)
	if err != nil {
		return nil, err
	}

	if ctx.Done() == nil {
		ctx = w.Context()
	}

	stream, err := w.openUniStreamFunc(ctx)
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			sessErr := &SessionError{
				ApplicationError: appErr,
			}
			return nil, sessErr
		}
		return nil, err
	}

	stream.SetPriority(urgencyFor(config.Priority))

	err = message.StreamTypeGroup.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := transport.StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	err = message.GroupMessage{
		SubscribeID:   uint64(w.subscribeStream.subscribeID),
		GroupSequence: uint64(seq),
		FrameStart:    frameStart,
	}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := transport.StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	gw := newGroupWriter(stream, seq, w.groupManager)
	gw.frameStart, gw.frameEnd = frameStart, frameEnd
	if w.ages != nil {
		gw.ages = w.ages
		w.ages.add(gw)
	}
	return gw, nil
}

// ErrGroupOutOfRange is returned by TrackWriter.OpenGroup and OpenGroupAt for
// a group outside the subscription's range: below its StartGroup floor or
// above its EndGroup.
var ErrGroupOutOfRange = errors.New("moqt: group outside the subscription's range")
