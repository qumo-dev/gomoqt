package moqt

import (
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

func newReceiveSubscribeStream(id SubscribeID, stream transport.Stream, config *SubscribeConfig) *receiveSubscribeStream {
	substr := &receiveSubscribeStream{
		subscribeID: id,
		config:      config,
		stream:      stream,
		updatedCh:   make(chan struct{}, 1),
	}

	// Listen for updates in a separate goroutine
	go func() {
		var updateMsg message.SubscribeUpdateMessage
		var err error

		for {
			err = updateMsg.Decode(substr.stream)
			if err != nil {
				break
			}

			config := &SubscribeConfig{
				Priority:   TrackPriority(updateMsg.SubscriberPriority),
				Ordered:    boolFromWireFlag(updateMsg.SubscriberOrdered),
				MaxLatency: updateMsg.SubscriberMaxLatency,
				StartGroup: groupSequenceFromWire(updateMsg.GroupStart),
				EndGroup:   groupSequenceFromWire(updateMsg.GroupEnd),
			}

			substr.mu.Lock()

			substr.config = config
			select {
			case substr.updatedCh <- struct{}{}:
			default:
			}
			substr.mu.Unlock()
		}
	}()

	return substr
}

type receiveSubscribeStream struct {
	subscribeID SubscribeID

	stream transport.Stream

	mu sync.Mutex

	config          *SubscribeConfig
	updatedCh       chan struct{}
	responseStarted bool
	endSent         bool
}

func (substr *receiveSubscribeStream) SubscribeID() SubscribeID {
	return substr.subscribeID
}

// ensureOk sends SUBSCRIBE_OK with the resolved start group exactly once.
// Subsequent calls are no-ops.
func (substr *receiveSubscribeStream) ensureOk(group GroupSequence) error {
	substr.mu.Lock()
	if substr.responseStarted {
		substr.mu.Unlock()
		return nil
	}
	err := substr.writeOkLocked(group)
	substr.mu.Unlock()

	if err != nil {
		_ = substr.closeWithError(SubscribeErrorCodeInternal)
	}

	return err
}

// writeOkLocked writes the type tag and SUBSCRIBE_OK message.
// Caller MUST hold substr.mu.
func (substr *receiveSubscribeStream) writeOkLocked(group GroupSequence) error {
	if _, err := substr.stream.Write([]byte{byte(message.MessageTypeSubscribeOk)}); err != nil {
		return err
	}

	err := message.SubscribeOkMessage{
		Group: uint64(group),
	}.Encode(substr.stream)
	if err != nil {
		return err
	}

	substr.responseStarted = true

	return nil
}

// writeEnd sends SUBSCRIBE_END with the last group that may be delivered.
// Per moq-lite-05, SUBSCRIBE_END without a preceding SUBSCRIBE_OK signals a
// track that ended with no matching groups.
func (substr *receiveSubscribeStream) writeEnd(group GroupSequence) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if substr.endSent {
		return nil
	}

	if _, err := substr.stream.Write([]byte{byte(message.MessageTypeSubscribeEnd)}); err != nil {
		return err
	}

	err := message.SubscribeEndMessage{
		Group: uint64(group),
	}.Encode(substr.stream)
	if err != nil {
		return err
	}

	substr.endSent = true

	return nil
}

func (substr *receiveSubscribeStream) writeDrop(drop SubscribeDrop) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if !substr.responseStarted {
		// A leading range is dropped implicitly by SUBSCRIBE_OK: resolving
		// the start group past the dropped range makes an explicit
		// SUBSCRIBE_DROP unnecessary.
		return substr.writeOkLocked(drop.EndGroup.Next())
	}

	if _, err := substr.stream.Write([]byte{byte(message.MessageTypeSubscribeDrop)}); err != nil {
		return err
	}

	// SUBSCRIBE_DROP carries plain absolute sequences, not the +1 form
	// used by SUBSCRIBE.
	err := message.SubscribeDropMessage{
		GroupStart: uint64(drop.StartGroup),
		GroupEnd:   uint64(drop.EndGroup),
		ErrorCode:  uint64(drop.ErrorCode),
	}.Encode(substr.stream)
	if err != nil {
		return err
	}

	return nil
}

func (substr *receiveSubscribeStream) TrackConfig() *SubscribeConfig {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	// Ensure config is never nil
	if substr.config == nil {
		substr.config = &SubscribeConfig{}
	}

	return substr.config
}

func (substr *receiveSubscribeStream) Updated() <-chan struct{} {
	return substr.updatedCh
}

func (substr *receiveSubscribeStream) close() error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if updateCh := substr.updatedCh; updateCh != nil {
		substr.updatedCh = nil
		close(updateCh)
	}

	return substr.stream.Close()
}

func (substr *receiveSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	strErrCode := transport.StreamErrorCode(code)
	cancelStreamWithError(substr.stream, strErrCode)

	if updateCh := substr.updatedCh; updateCh != nil {
		substr.updatedCh = nil
		close(updateCh)
	}

	return nil
}
