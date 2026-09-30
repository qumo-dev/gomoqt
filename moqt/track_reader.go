package moqt

import (
	"context"
	"errors"
	"io"
	"iter"
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
	// lastQueued is the highest group sequence queued so far (0: none).
	// Group sequences start at 1.
	lastQueued GroupSequence

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

// subscribeEndGrace is how long AcceptGroup still waits for the last group a
// SUBSCRIBE_END named after the publisher closed the subscribe stream: QUIC
// does not order the group's stream against that close, so a group written
// just before it can arrive just after. A publisher that closed while opening
// a group names one that never comes; the grace bounds that wait.
const subscribeEndGrace = 100 * time.Millisecond

// AcceptGroup blocks until the next group is available or context is
// canceled. It returns a GroupReader tied to the accepted group stream.
//
// When the publisher ends the track (SUBSCRIBE_END), AcceptGroup keeps
// returning the groups still queued, and those still in flight up to the last
// one it named, then returns io.EOF. SUBSCRIBE_END promises only that no later
// group will come, not that the named one will: once the publisher has also
// closed the subscribe stream, AcceptGroup waits at most subscribeEndGrace for
// it, since its group stream may still be in flight behind the close.
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
		// The publisher ended the track, and either its last group has been
		// handed out (or it had none) or it has closed the subscribe stream:
		// nothing more will arrive.
		end, ended := r.sendSubscribeStream.end()
		closedAt, closed := r.sendSubscribeStream.closedSince()
		var graceLeft <-chan time.Time
		if ended {
			if r.lastQueued >= end {
				r.trackMu.Unlock()
				return nil, io.EOF
			}
			if closed {
				left := subscribeEndGrace - time.Since(closedAt)
				if left <= 0 {
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

	if r.Context().Err() != nil || r.queueing == nil {
		stream.CancelRead(transport.StreamErrorCode(SubscribeCanceledErrorCode))
		return
	}

	entry := struct {
		sequence GroupSequence
		stream   transport.ReceiveStream
	}{
		sequence: sequence,
		stream:   stream,
	}
	r.queueing = append(r.queueing, entry)
	if sequence > r.lastQueued {
		r.lastQueued = sequence
	}

	select {
	case r.queuedCh <- struct{}{}:
	default:
	}
}
