package moqt

import (
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

func newReceiveSubscribeStream(id SubscribeID, stream transport.Stream, config *SubscribeConfig) *receiveSubscribeStream {
	return &receiveSubscribeStream{
		subscribeID: id,
		config:      config,
		stream:      stream,
	}
}

type receiveSubscribeStream struct {
	subscribeID SubscribeID

	stream transport.Stream

	readMu sync.Mutex // serializes readUpdate so concurrent callers can't interleave Decodes on the stream
	mu     sync.Mutex // guards config

	config          *SubscribeConfig
	responseStarted bool
}

// readUpdate blocks until the peer sends the next SUBSCRIBE_UPDATE, applies it
// as the current config, and returns that config. It returns an error once the
// subscribe stream ends or is closed (the terminal signal for an update-reading
// loop).
//
// It reads the stream one message at a time. readMu serializes concurrent
// callers so their Decodes cannot interleave, but a publisher that wants to
// follow updates should still call it from a single goroutine — concurrent
// callers each receive a distinct update in an unspecified order. A
// subscription whose publisher never calls it (the common relay fan-out case)
// spends no goroutine on update reading at all.
func (substr *receiveSubscribeStream) readUpdate() (*SubscribeConfig, error) {
	substr.readMu.Lock()
	defer substr.readMu.Unlock()

	var updateMsg message.SubscribeUpdateMessage
	if err := updateMsg.Decode(substr.stream); err != nil {
		return nil, err
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
	substr.mu.Unlock()

	return config, nil
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

func (substr *receiveSubscribeStream) close() error {
	return substr.stream.Close()
}

func (substr *receiveSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	cancelStreamWithError(substr.stream, transport.StreamErrorCode(code))
	return nil
}
