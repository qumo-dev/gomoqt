package moqt

import (
	"fmt"
	"io"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/moqttrace"
	"github.com/qumo-dev/gomoqt/transport"
)

func newSendSubscribeStream(id SubscribeID, stream transport.Stream, config *SubscribeConfig, trace *moqttrace.SessionTrace) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		id:        id,
		config:    config,
		stream:    stream,
		droppedCh: make(chan struct{}, 1),
		trace:     trace,
	}

	return substr
}

type sendSubscribeStream struct {
	stream transport.Stream

	config *SubscribeConfig

	info PublishInfo

	mu sync.Mutex

	droppedCh chan struct{}
	drops     []SubscribeDrop

	id SubscribeID

	trace *moqttrace.SessionTrace
}

func (substr *sendSubscribeStream) readSubscribeResponses() {
	for {
		ok, drop, err := readSubscribeResponse(substr.stream)
		if err != nil {
			return
		}

		if ok != nil {
			substr.updateInfo(PublishInfo{
				Priority:   TrackPriority(ok.PublisherPriority),
				Ordered:    boolFromWireFlag(ok.PublisherOrdered),
				MaxLatency: ok.PublisherMaxLatency,
				StartGroup: groupSequenceFromWire(ok.StartGroup),
				EndGroup:   groupSequenceFromWire(ok.EndGroup),
			})
			continue
		}

		if drop != nil {
			substr.appendDrop(SubscribeDrop{
				StartGroup: groupSequenceFromWire(drop.StartGroup),
				EndGroup:   groupSequenceFromWire(drop.EndGroup),
				ErrorCode:  SubscribeErrorCode(drop.ErrorCode),
			})
			return
		}
	}
}

func readSubscribeResponse(stream io.Reader) (*message.SubscribeOkMessage, *message.SubscribeDropMessage, error) {
	head := make([]byte, 1)
	if _, err := io.ReadFull(stream, head); err != nil {
		return nil, nil, err
	}

	msgType, _, err := message.ReadVarint(head)
	if err != nil {
		return nil, nil, err
	}

	switch msgType {
	case 0x0:
		var msg message.SubscribeOkMessage
		err := msg.Decode(stream)
		if err != nil {
			return nil, nil, err
		}
		return &msg, nil, nil
	case 0x1:
		var msg message.SubscribeDropMessage
		err := msg.Decode(stream)
		if err != nil {
			return nil, nil, err
		}
		return nil, &msg, nil
	default:
		return nil, nil, fmt.Errorf("unexpected SUBSCRIBE response type: %d", msgType)
	}
}

func (substr *sendSubscribeStream) SubscribeID() SubscribeID {
	return substr.id
}

func (substr *sendSubscribeStream) TrackConfig() *SubscribeConfig {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	return substr.config
}

func (substr *sendSubscribeStream) updateInfo(newInfo PublishInfo) {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	substr.info = newInfo
}

func (substr *sendSubscribeStream) updateSubscribe(newConfig *SubscribeConfig) error {
	if newConfig == nil {
		// TODO: Handle nil config case if necessary (e.g., return an error, ignore the update, etc.)
		return nil
	}

	// Send the message first before updating config
	ordered := boolToWireFlag(newConfig.Ordered)

	startGroup := groupSequenceToWire(newConfig.StartGroup)

	endGroup := groupSequenceToWire(newConfig.EndGroup)

	sum := message.SubscribeUpdateMessage{
		SubscriberPriority:   uint8(newConfig.Priority),
		SubscriberOrdered:    ordered,
		SubscriberMaxLatency: newConfig.MaxLatency,
		StartGroup:           startGroup,
		EndGroup:             endGroup,
	}
	err := sum.Encode(substr.stream)
	if err != nil {
		substr.closeWithError(SubscribeErrorCodeInternal)
		return err
	}

	substr.mu.Lock()
	substr.config = newConfig
	substr.mu.Unlock()

	return nil
}

func (substr *sendSubscribeStream) appendDrop(drop SubscribeDrop) {
	substr.mu.Lock()
	substr.drops = append(substr.drops, drop)
	select {
	case substr.droppedCh <- struct{}{}:
	default:
	}
	substr.mu.Unlock()
}

func (substr *sendSubscribeStream) pendingDrops() []SubscribeDrop {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if len(substr.drops) == 0 {
		return nil
	}

	drops := substr.drops
	substr.drops = nil
	return drops
}

func (substr *sendSubscribeStream) ReadInfo() PublishInfo {
	return substr.info
}

func (substr *sendSubscribeStream) close() error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	return substr.stream.Close()
}

func (substr *sendSubscribeStream) closeWithError(code SubscribeErrorCode) {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	cancelStreamWithError(substr.stream, transport.StreamErrorCode(code))
}
