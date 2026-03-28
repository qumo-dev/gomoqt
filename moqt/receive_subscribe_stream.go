package moqt

import (
	"context"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

func newReceiveSubscribeStream(id SubscribeID, stream Stream, config *TrackConfig) *receiveSubscribeStream {
	// Ensure config is not nil
	if config == nil {
		config = &TrackConfig{}
	}

	rss := &receiveSubscribeStream{
		subscribeID: id,
		config:      config,
		stream:      stream,
		updatedCh:   make(chan struct{}, 1),
		closeOnce:   make(chan struct{}, 1),
		ctx:         context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeSubscribe),
	}

	// Listen for updates in a separate goroutine
	go func() {
		var sum message.SubscribeUpdateMessage
		var err error

		for {
			rss.configMu.Lock()
			if rss.ctx.Err() != nil {
				rss.configMu.Unlock()
				break
			}
			rss.configMu.Unlock()

			err = sum.Decode(rss.stream)
			if err != nil {
				break
			}

			rss.configMu.Lock()
			ordered := false
			if sum.SubscriberOrdered != 0 {
				ordered = true
			}

			startGroup := GroupSequence(0)
			if sum.StartGroup != 0 {
				startGroup = GroupSequence(sum.StartGroup - 1)
			}

			endGroup := GroupSequence(0)
			if sum.EndGroup != 0 {
				endGroup = GroupSequence(sum.EndGroup - 1)
			}

			rss.config = &TrackConfig{
				TrackPriority: TrackPriority(sum.SubscriberPriority),
				Ordered:       ordered,
				MaxLatencyMs:  sum.SubscriberMaxLatency,
				StartGroup:    startGroup,
				EndGroup:      endGroup,
			}

			select {
			case rss.updatedCh <- struct{}{}:
			default:
			}
			rss.configMu.Unlock()
		}

		// Cleanup after loop ends
		rss.configMu.Lock()
		select {
		case rss.closeOnce <- struct{}{}:
			if rss.updatedCh != nil {
				close(rss.updatedCh)
			}
		default:
		}
		rss.configMu.Unlock()

	}()

	return rss
}

type receiveSubscribeStream struct {
	subscribeID SubscribeID

	stream Stream

	acceptOnce sync.Once
	// writeInfoWG tracks active WriteInfo calls so close waits for them.
	writeInfoWG sync.WaitGroup

	configMu  sync.Mutex
	config    *TrackConfig
	updatedCh chan struct{}

	closeOnce chan struct{}

	ctx context.Context
}

func (rss *receiveSubscribeStream) SubscribeID() SubscribeID {
	return rss.subscribeID
}

func (rss *receiveSubscribeStream) writeInfo(info Info) error {
	var err error
	rss.acceptOnce.Do(func() {
		rss.writeInfoWG.Add(1)
		defer rss.writeInfoWG.Done()
		rss.configMu.Lock()
		defer rss.configMu.Unlock()
		if rss.ctx.Err() != nil {
			err = Cause(rss.ctx)
			return
		}
		ordered := uint8(0)
		if rss.config.Ordered {
			ordered = 1
		}

		startGroup := uint64(0)
		if rss.config.StartGroup != 0 {
			startGroup = uint64(rss.config.StartGroup) + 1
		}

		endGroup := uint64(0)
		if rss.config.EndGroup != 0 {
			endGroup = uint64(rss.config.EndGroup) + 1
		}

		sum := message.SubscribeOkMessage{
			PublisherPriority:   uint8(rss.config.TrackPriority),
			PublisherOrdered:    ordered,
			PublisherMaxLatency: rss.config.MaxLatencyMs,
			StartGroup:          startGroup,
			EndGroup:            endGroup,
		}
		err = sum.Encode(rss.stream)
		if err != nil {
			_ = rss.closeWithError(InternalSubscribeErrorCode)
			return
		}
	})

	return err
}

func (rss *receiveSubscribeStream) TrackConfig() *TrackConfig {
	rss.configMu.Lock()
	defer rss.configMu.Unlock()

	// Ensure config is never nil
	if rss.config == nil {
		rss.config = &TrackConfig{}
	}

	return rss.config
}

func (rss *receiveSubscribeStream) Updated() <-chan struct{} {
	return rss.updatedCh
}

func (rss *receiveSubscribeStream) Context() context.Context {
	if rss == nil || rss.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return rss.ctx
}

func (rss *receiveSubscribeStream) close() error {
	rss.configMu.Lock()
	defer rss.configMu.Unlock()

	if rss.ctx.Err() != nil {
		return Cause(rss.ctx)
	}

	// Wait for any in-flight WriteInfo calls to finish before closing.
	rss.writeInfoWG.Wait()

	// Close the write-side stream. Do not cancel the read side for a
	// graceful close: allow the peer to finish its read operations and
	// close the stream gracefully to avoid triggering a reset.
	err := rss.stream.Close()

	select {
	case rss.closeOnce <- struct{}{}:
		if rss.updatedCh != nil {
			close(rss.updatedCh)
		}
	default:
	}

	return err
}

func (rss *receiveSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	if rss == nil {
		panic("receiveSubscribeStream: cannot call closeWithError on nil stream")
	}

	rss.configMu.Lock()
	defer rss.configMu.Unlock()

	if rss.ctx.Err() != nil {
		return Cause(rss.ctx)
	}

	// Wait for any in-flight WriteInfo calls to finish. We still cancel the
	// stream afterwards to enforce the error unconditionally.
	rss.writeInfoWG.Wait()

	strErrCode := StreamErrorCode(code)
	// Cancel the write-side stream
	rss.stream.CancelWrite(strErrCode)
	// Cancel the read-side stream
	rss.stream.CancelRead(strErrCode)

	select {
	case rss.closeOnce <- struct{}{}:
		if rss.updatedCh != nil {
			close(rss.updatedCh)
		}
	default:
	}

	return nil
}
