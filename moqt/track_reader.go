package moqt

import (
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

type groupReaderManager struct {
	mu           sync.Mutex
	activeGroups map[*GroupReader]struct{}
	closed       bool
}

func newGroupReaderManager() *groupReaderManager {
	return &groupReaderManager{
		activeGroups: make(map[*GroupReader]struct{}),
	}
}

func (m *groupReaderManager) addGroup(group *GroupReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.activeGroups[group] = struct{}{}
}

func (m *groupReaderManager) removeGroup(group *GroupReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.activeGroups, group)
}

func newTrackReader(path BroadcastPath, name TrackName, subscribeStream *sendSubscribeStream, onCloseFunc func()) *TrackReader {
	track := &TrackReader{
		BroadcastPath:       path,
		TrackName:           name,
		sendSubscribeStream: subscribeStream,
		queuedCh:            make(chan struct{}, 1),
		queueing: make([]struct {
			sequence GroupSequence
			stream   transport.ReceiveStream
		}, 0, 1<<3),
		dequeued:     make(map[*GroupReader]struct{}),
		groupManager: newGroupReaderManager(),
		onCloseFunc:  onCloseFunc,
		ctx:          context.WithValue(subscribeStream.stream.Context(), biStreamTypeCtxKey, message.StreamTypeSubscribe),
	}
	// Set before readSubscribeResponses starts, which is the only caller.
	subscribeStream.onDrop = track.settleDropped

	return track
}

// TrackReader receives groups for a subscribed track.
// It queues incoming group streams and allows the application to accept them via AcceptGroup.
// TrackReader provides lifecycle and update APIs for managing subscriptions.
type TrackReader struct {
	// BroadcastPath is the path of the broadcast this subscription targets.
	// The value is set at subscription time and does not change.
	BroadcastPath BroadcastPath

	// TrackName is the name of the track within the broadcast.
	// The value is set at subscription time and does not change.
	TrackName TrackName

	sendSubscribeStream *sendSubscribeStream

	queueing []struct {
		sequence GroupSequence
		stream   transport.ReceiveStream
	}
	queuedCh chan struct{}
	trackMu  sync.Mutex
	// settled records the groups received or dropped (SUBSCRIBE_DROP), so
	// that SUBSCRIBE_END ends the subscription only once no earlier group is
	// still owed. Guarded by trackMu.
	settled settledGroups
	// eof is set once AcceptGroup has returned io.EOF; it keeps returning it,
	// and groups that arrive later are cancelled rather than queued.
	eof bool
	// endGrace returns how long to wait for a missing group after the
	// publisher closed the subscribe stream; nil means subscribeEndGrace.
	endGrace func() time.Duration

	dequeued map[*GroupReader]struct{}

	groupManager *groupReaderManager
	onCloseFunc  func()

	ctx context.Context
}

func (r *TrackReader) SubscribeID() SubscribeID {
	return r.sendSubscribeStream.SubscribeID()
}

func (r *TrackReader) TrackConfig() *SubscribeConfig {
	return r.sendSubscribeStream.TrackConfig()
}

// ResolvedStart returns the absolute start group resolved by the publisher's
// SUBSCRIBE_OK, or MinGroupSequence before SUBSCRIBE_OK arrives.
func (r *TrackReader) ResolvedStart() GroupSequence {
	return r.sendSubscribeStream.resolvedStartGroup()
}

// Ended reports whether the publisher signaled SUBSCRIBE_END, meaning no group
// after the resolved range will be produced.
func (r *TrackReader) Ended() bool {
	return r.sendSubscribeStream.hasEnded()
}

// acceptDrop blocks until a drop notification is available or context is canceled.
func (r *TrackReader) acceptDrop(ctx context.Context) (SubscribeDrop, error) {
	trackCtx := r.Context()

	for {
		if drops := r.sendSubscribeStream.pendingDrops(); len(drops) > 0 {
			// Re-append remaining drops
			for _, d := range drops[1:] {
				r.sendSubscribeStream.appendDrop(d)
			}
			return drops[0], nil
		}

		if trackCtx.Err() != nil {
			return SubscribeDrop{}, Cause(trackCtx)
		}

		select {
		case <-ctx.Done():
			return SubscribeDrop{}, ctx.Err()
		case <-trackCtx.Done():
			return SubscribeDrop{}, Cause(trackCtx)
		case <-r.sendSubscribeStream.droppedCh:
		}
	}
}

// Drops returns an iterator that yields SubscribeDrop values until ctx or
// the reader's context is canceled.
func (r *TrackReader) Drops(ctx context.Context) iter.Seq[SubscribeDrop] {
	return func(yield func(SubscribeDrop) bool) {
		for {
			drop, err := r.acceptDrop(ctx)
			if err != nil {
				return
			}

			if !yield(drop) {
				return
			}
		}
	}
}

// subscribeEndGrace is the least time AcceptGroup still waits for a group
// SUBSCRIBE_END covers after the publisher closed the subscribe stream: QUIC
// does not order a group's stream against that close, so a group written just
// before it can arrive just after. A group that never comes (skipped without
// SUBSCRIBE_DROP, or abandoned mid-open) would otherwise be waited for
// forever; the grace bounds that wait. A Session adds three round trips, so a
// loss recovery on a slow path is not cut short.
const subscribeEndGrace = 100 * time.Millisecond

// subscribeEndGraceRTTs is how many round trips a Session adds to
// subscribeEndGrace: a lost packet on the group's stream is recovered within
// about one round trip plus the probe timeout.
const subscribeEndGraceRTTs = 3

// AcceptGroup blocks until the next group is available or context is
// canceled. It returns a GroupReader tied to the accepted group stream.
//
// When the publisher ends the track (SUBSCRIBE_END), AcceptGroup returns the
// groups still queued and still in flight, then io.EOF once every group from
// the subscription's start to the last one SUBSCRIBE_END named has arrived or
// been dropped (SUBSCRIBE_DROP). Group streams arrive in any order, so EOF
// waits for the gaps, not only for the named group. A gap that never fills
// is waited for until the publisher has closed the subscribe stream and a
// grace period has passed. io.EOF is sticky: later calls return it again,
// and a group that arrives after it is cancelled.
func (r *TrackReader) AcceptGroup(ctx context.Context) (*GroupReader, error) {
	trackCtx := r.Context()

	for {
		r.trackMu.Lock()
		if len(r.queueing) > 0 {
			next := r.queueing[0]

			r.queueing = r.queueing[1:]

			group := newGroupReader(next.sequence, next.stream, r.groupManager)

			r.trackMu.Unlock()
			return group, nil
		}
		// Read queuedCh under the lock: Close sets it to nil concurrently.
		queued := r.queuedCh
		if r.eof {
			r.trackMu.Unlock()
			return nil, io.EOF
		}
		// The publisher ended the track: nothing more will arrive once every
		// group it covered has arrived or been dropped, or, for a gap that
		// never fills, once the stream has closed and the grace has passed.
		end, ended := r.sendSubscribeStream.end()
		closedAt, closed := r.sendSubscribeStream.closedSince()
		var graceLeft <-chan time.Time
		if ended {
			start := r.sendSubscribeStream.resolvedStartGroup()
			// start is 0 when SUBSCRIBE_END came without SUBSCRIBE_OK: the
			// track ended with no groups for this subscription.
			if start == 0 || end < start || r.settled.covers(start, end) {
				r.eof = true
				r.trackMu.Unlock()
				return nil, io.EOF
			}
			if closed {
				left := r.graceAfterClose() - time.Since(closedAt)
				if left <= 0 {
					r.eof = true
					r.trackMu.Unlock()
					return nil, io.EOF
				}
				graceLeft = time.After(left)
			}
		}
		// Once SUBSCRIBE_END has arrived, endCh stays closed; waiting on it
		// again would spin. What can still change is a new group or the
		// stream closing.
		endCh := r.sendSubscribeStream.endCh
		if ended {
			endCh = nil
		}
		// Likewise closedCh once the close has been seen.
		closedCh := r.sendSubscribeStream.closedCh
		if closed {
			closedCh = nil
		}
		r.trackMu.Unlock()

		if trackCtx.Err() != nil {
			return nil, Cause(trackCtx)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-trackCtx.Done():
			return nil, Cause(trackCtx)
		case <-queued:
		case <-endCh:
		case <-closedCh:
		case <-graceLeft:
		}
	}
}

func (r *TrackReader) Context() context.Context {
	return r.ctx
}

// Close cancels queued groups, closes the queued channel, and terminates
// the subscription stream gracefully.
func (r *TrackReader) Close() error {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	// Cancel all pending groups first
	errCode := transport.StreamErrorCode(SubscribeCanceledErrorCode)
	for _, entry := range r.queueing {
		entry.stream.CancelRead(errCode)
	}
	r.queueing = nil

	// Cancel all dequeued groups
	for stream := range r.dequeued {
		stream.CancelRead(SubscribeCanceledErrorCode)
	}
	r.dequeued = nil

	if r.queuedCh != nil {
		close(r.queuedCh)
		r.queuedCh = nil
	}

	r.onCloseFunc()

	return r.sendSubscribeStream.close()
}

// CloseWithError cancels the subscription with the provided SubscribeErrorCode and terminates the subscription.
func (r *TrackReader) CloseWithError(code SubscribeErrorCode) {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	// Cancel all pending groups first
	errCode := transport.StreamErrorCode(code)
	for _, entry := range r.queueing {
		entry.stream.CancelRead(errCode)
	}
	r.queueing = nil

	// Cancel all dequeued groups
	for stream := range r.dequeued {
		stream.CancelRead(SubscribeCanceledErrorCode)
	}
	r.dequeued = nil

	if r.queuedCh != nil {
		close(r.queuedCh)
		r.queuedCh = nil
	}

	r.onCloseFunc()

	r.sendSubscribeStream.closeWithError(code)
}

// Update updates the subscription configuration with a new TrackConfig.
func (r *TrackReader) Update(config *SubscribeConfig) error {
	if config == nil {
		return errors.New("subscribe config cannot be nil")
	}

	return r.sendSubscribeStream.updateSubscribe(config)
}

func (r *TrackReader) enqueueGroup(sequence GroupSequence, stream transport.ReceiveStream) {
	if stream == nil {
		return
	}

	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	if r.Context().Err() != nil || r.queueing == nil || r.eof {
		stream.CancelRead(transport.StreamErrorCode(SubscribeCanceledErrorCode))
		return
	}
	r.settled.add(sequence, sequence)

	entry := struct {
		sequence GroupSequence
		stream   transport.ReceiveStream
	}{
		sequence: sequence,
		stream:   stream,
	}
	r.queueing = append(r.queueing, entry)

	select {
	case r.queuedCh <- struct{}{}:
	default:
	}
}

// settleDropped records a SUBSCRIBE_DROP range as settled and wakes a waiting
// AcceptGroup, which may now reach io.EOF.
func (r *TrackReader) settleDropped(drop SubscribeDrop) {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()
	r.settled.add(drop.StartGroup, drop.EndGroup)
	select {
	case r.queuedCh <- struct{}{}:
	default:
	}
}

// graceAfterClose is how long a gap is still waited for after the publisher
// closed the subscribe stream.
func (r *TrackReader) graceAfterClose() time.Duration {
	if r.endGrace != nil {
		return r.endGrace()
	}
	return subscribeEndGrace
}

// maxSettledRanges bounds settledGroups. A publisher that skips groups without
// SUBSCRIBE_DROP leaves permanent gaps; past this many, the oldest gap, long
// behind any reordering, is treated as settled.
const maxSettledRanges = 1024

// settledGroups is a set of group sequences kept as sorted, disjoint,
// inclusive ranges, merged as they touch. In-order delivery keeps one range.
type settledGroups struct {
	ranges []groupRange
}

// groupRange is an inclusive range of group sequences.
type groupRange struct{ lo, hi GroupSequence }

// add records lo through hi as settled.
func (s *settledGroups) add(lo, hi GroupSequence) {
	if hi < lo {
		return
	}
	// Most groups arrive in order, so search from the end.
	i := len(s.ranges)
	for i > 0 && s.ranges[i-1].lo > lo {
		i--
	}
	s.ranges = slices.Insert(s.ranges, i, groupRange{lo: lo, hi: hi})
	if i > 0 && s.ranges[i-1].hi+1 >= lo {
		i--
	}
	j := i + 1
	for j < len(s.ranges) && s.ranges[j].lo <= s.ranges[i].hi+1 {
		s.ranges[i].hi = max(s.ranges[i].hi, s.ranges[j].hi)
		j++
	}
	s.ranges = slices.Delete(s.ranges, i+1, j)
	if len(s.ranges) > maxSettledRanges {
		s.ranges[1].lo = s.ranges[0].lo
		s.ranges = slices.Delete(s.ranges, 0, 1)
	}
}

// covers reports whether every sequence from lo through hi is settled.
func (s *settledGroups) covers(lo, hi GroupSequence) bool {
	for _, r := range s.ranges {
		if r.lo <= lo && hi <= r.hi {
			return true
		}
	}
	return false
}
