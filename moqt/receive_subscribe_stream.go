package moqt

import (
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
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
				StartGroup: groupSequenceFromWire(updateMsg.StartGroup),
				EndGroup:   groupSequenceFromWire(updateMsg.EndGroup),
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
}

func (substr *receiveSubscribeStream) SubscribeID() SubscribeID {
	return substr.subscribeID
}

func (substr *receiveSubscribeStream) ensureInfo(info PublishInfo) error {
	substr.mu.Lock()
	if substr.responseStarted {
		substr.mu.Unlock()
		return nil
	}
	err := substr.writeInfoLocked(info)
	substr.mu.Unlock()

	if err != nil {
		_ = substr.closeWithError(SubscribeErrorCodeInternal)
	}

	return err
}

func (substr *receiveSubscribeStream) writeInfo(info PublishInfo) error {
	substr.mu.Lock()
	err := substr.writeInfoLocked(info)
	substr.mu.Unlock()

	if err != nil {
		_ = substr.closeWithError(SubscribeErrorCodeInternal)
	}

	return err
}

func (substr *receiveSubscribeStream) writeInfoLocked(info PublishInfo) error {
	if _, err := substr.stream.Write([]byte{byte(message.MessageTypeSubscribeOk)}); err != nil {
		return err
	}

	err := message.SubscribeOkMessage{
		PublisherPriority:   uint8(info.Priority),
		PublisherOrdered:    boolToWireFlag(info.Ordered),
		PublisherMaxLatency: info.MaxLatency,
		StartGroup:          groupSequenceToWire(info.StartGroup),
		EndGroup:            groupSequenceToWire(info.EndGroup),
	}.Encode(substr.stream)

	if err != nil {
		return err
	}

	substr.responseStarted = true

	return nil
}

func (substr *receiveSubscribeStream) writeDrop(drop SubscribeDrop) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if !substr.responseStarted {
		err := substr.writeInfoLocked(PublishInfo{})
		if err != nil {
			return err
		}
	}

	if _, err := substr.stream.Write([]byte{byte(message.MessageTypeSubscribeDrop)}); err != nil {
		return err
	}

	err := message.SubscribeDropMessage{
		StartGroup: groupSequenceToWire(drop.StartGroup),
		EndGroup:   groupSequenceToWire(drop.EndGroup),
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
