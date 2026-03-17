package moqt

import (
	"context"
	"errors"
	"sync"

	"github.com/okdaichi/gomoqt/transport"
)

func newTrackReader(broadcastPath BroadcastPath, trackName TrackName, subscribeStream *sendSubscribeStream, onCloseTrackFunc func()) *TrackReader {
	track := &TrackReader{
		BroadcastPath:       broadcastPath,
		TrackName:           trackName,
		sendSubscribeStream: subscribeStream,
		queuedCh:            make(chan struct{}, 1),
		queueing: make([]struct {
			sequence GroupSequence
			stream   transport.ReceiveStream
		}, 0, 1<<3),
		dequeued:         make(map[*GroupReader]struct{}),
		onCloseTrackFunc: onCloseTrackFunc,
	}

	return track
}

// TrackReader receives groups for a subscribed track.
// It queues incoming group streams and allows the application to accept them via AcceptGroup.
// TrackReader provides lifecycle and update APIs for managing subscriptions.
type TrackReader struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName

	sendSubscribeStream *sendSubscribeStream

	queueing []struct {
		sequence GroupSequence
		stream   transport.ReceiveStream
	}
	queuedCh chan struct{}
	trackMu  sync.Mutex

	dequeued map[*GroupReader]struct{}

	onCloseTrackFunc func()
}

func (r *TrackReader) SubscribeID() SubscribeID {
	return r.sendSubscribeStream.SubscribeID()
}

func (r *TrackReader) TrackConfig() *TrackConfig {
	return r.sendSubscribeStream.TrackConfig()
}

// AcceptGroup blocks until the next group is available or context is
// canceled. It returns a GroupReader tied to the accepted group stream.
func (r *TrackReader) AcceptGroup(ctx context.Context) (*GroupReader, error) {
	trackCtx := r.Context()

	for {
		group := r.dequeueGroup()
		if group != nil {
			r.addGroup(group)

			return group, nil
		}

		if trackCtx.Err() != nil {
			return nil, Cause(trackCtx)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-trackCtx.Done():
			return nil, Cause(trackCtx)
		case <-r.queuedCh:
		}
	}
}

func (r *TrackReader) Context() context.Context {
	return r.sendSubscribeStream.Context()
}

func (r *TrackReader) dequeueGroup() *GroupReader {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	if len(r.queueing) > 0 {
		next := r.queueing[0]

		r.queueing = r.queueing[1:]

		var group *GroupReader
		group = newGroupReader(next.sequence, next.stream,
			func() { r.removeGroup(group) })

		return group
	}

	return nil
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

	r.onCloseTrackFunc()

	return r.sendSubscribeStream.close()
}

// CloseWithError cancels the subscription with the provided SubscribeErrorCode and terminates the subscription.
func (r *TrackReader) CloseWithError(code SubscribeErrorCode) error {
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

	r.onCloseTrackFunc()

	return r.sendSubscribeStream.closeWithError(code)
}

// Update updates the subscription configuration with a new TrackConfig.
func (r *TrackReader) Update(config *TrackConfig) error {
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

	select {
	case r.queuedCh <- struct{}{}:
	default:
	}
}

func (r *TrackReader) addGroup(group *GroupReader) {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	r.dequeued[group] = struct{}{}
}

func (r *TrackReader) removeGroup(group *GroupReader) {
	r.trackMu.Lock()
	defer r.trackMu.Unlock()

	delete(r.dequeued, group)
}
