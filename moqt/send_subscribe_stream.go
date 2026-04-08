package moqt

import (
	"context"
	"errors"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

func newSendSubscribeStream(id SubscribeID, stream Stream, initConfig *SubscribeConfig, info PublishInfo) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		ctx:    context.WithValue(stream.Context(), biStreamTypeCtxKey, message.StreamTypeSubscribe),
		id:     id,
		config: initConfig,
		stream: stream,
		info:   info,
	}

	return substr
}

type sendSubscribeStream struct {
	ctx context.Context

	config *SubscribeConfig

	stream Stream

	mu sync.Mutex

	info PublishInfo

	id SubscribeID
}

func (substr *sendSubscribeStream) SubscribeID() SubscribeID {
	return substr.id
}

func (substr *sendSubscribeStream) TrackConfig() *SubscribeConfig {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	return substr.config
}

func (substr *sendSubscribeStream) updateSubscribe(newConfig *SubscribeConfig) error {
	if newConfig == nil {
		return errors.New("new track config cannot be nil")
	}

	substr.mu.Lock()
	defer substr.mu.Unlock()

	if substr.ctx.Err() != nil {
		return Cause(substr.ctx)
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
		SubscriberPriority:   uint8(newConfig.Priority),
		SubscriberOrdered:    ordered,
		SubscriberMaxLatency: newConfig.MaxLatency,
		StartGroup:           startGroup,
		EndGroup:             endGroup,
	}
	err := sum.Encode(substr.stream)
	if err != nil {
		// Close the stream with error on write failure
		substr.mu.Unlock() // Unlock before calling closeWithError to avoid deadlock
		_ = substr.closeWithError(SubscribeErrorCodeInternal)
		substr.mu.Lock() // Re-lock for defer
		return err
	}

	substr.config = newConfig

	return nil
}

func (substr *sendSubscribeStream) ReadInfo() PublishInfo {
	return substr.info
}

func (substr *sendSubscribeStream) Context() context.Context {
	if substr == nil || substr.ctx == nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	return substr.ctx
}

func (substr *sendSubscribeStream) close() error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	// Close the write side of the stream
	err := substr.stream.Close()
	// Do not cancel the read side on a graceful close: allow peer to finish sending

	return err
}

func (substr *sendSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	strErrCode := StreamErrorCode(code)
	// Cancel the write side of the stream
	substr.stream.CancelWrite(strErrCode)
	// Cancel the read side of the stream
	substr.stream.CancelRead(strErrCode)

	return nil
}
