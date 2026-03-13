package moqt

import (
	"context"
	"errors"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/quic"
)

func newSendSubscribeStream(id SubscribeID, stream quic.Stream, initConfig *TrackConfig, info Info) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		ctx:    context.WithValue(stream.Context(), &biStreamTypeCtxKey, message.StreamTypeSubscribe),
		id:     id,
		config: initConfig,
		stream: stream,
		info:   info,
	}

	return substr
}

type sendSubscribeStream struct {
	ctx context.Context

	config *TrackConfig

	stream quic.Stream

	mu sync.Mutex

	info Info

	id SubscribeID
}

func (sss *sendSubscribeStream) SubscribeID() SubscribeID {
	return sss.id
}

func (sss *sendSubscribeStream) TrackConfig() *TrackConfig {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	return sss.config
}

func (sss *sendSubscribeStream) updateSubscribe(newConfig *TrackConfig) error {
	if newConfig == nil {
		return errors.New("new track config cannot be nil")
	}

	sss.mu.Lock()
	defer sss.mu.Unlock()

	if sss.ctx.Err() != nil {
		return Cause(sss.ctx)
	}

	// Send the message first before updating config
	ordered := uint8(0)
	if newConfig.Ordered {
		ordered = 1
	}

	startGroup := uint64(0)
	if newConfig.StartGroup != 0 {
		startGroup = uint64(newConfig.StartGroup) + 1
	}

	endGroup := uint64(0)
	if newConfig.EndGroup != 0 {
		endGroup = uint64(newConfig.EndGroup) + 1
	}

	sum := message.SubscribeUpdateMessage{
		SubscriberPriority:   uint8(newConfig.TrackPriority),
		SubscriberOrdered:    ordered,
		SubscriberMaxLatency: newConfig.MaxLatencyMs,
		StartGroup:           startGroup,
		EndGroup:             endGroup,
	}
	err := sum.Encode(sss.stream)
	if err != nil {
		// Close the stream with error on write failure
		sss.mu.Unlock() // Unlock before calling closeWithError to avoid deadlock
		_ = sss.closeWithError(InternalSubscribeErrorCode)
		sss.mu.Lock() // Re-lock for defer
		return err
	}

	// Only update config after successful message sending
	sss.config = newConfig

	return nil
}

func (sss *sendSubscribeStream) ReadInfo() Info {
	return sss.info
}

func (sss *sendSubscribeStream) Context() context.Context {
	if sss == nil || sss.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return sss.ctx
}

func (sss *sendSubscribeStream) close() error {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	// Close the write side of the stream
	err := sss.stream.Close()
	// Do not cancel the read side on a graceful close: allow peer to finish sending

	return err
}

func (sss *sendSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	sss.mu.Lock()
	defer sss.mu.Unlock()

	strErrCode := quic.StreamErrorCode(code)
	// Cancel the write side of the stream
	sss.stream.CancelWrite(strErrCode)
	// Cancel the read side of the stream
	sss.stream.CancelRead(strErrCode)

	return nil
}
