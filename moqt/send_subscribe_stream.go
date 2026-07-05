package moqt

import (
	"fmt"
	"io"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

func newSendSubscribeStream(id SubscribeID, stream transport.Stream, initConfig *SubscribeConfig) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		id:        id,
		config:    initConfig,
		stream:    stream,
		droppedCh: make(chan struct{}, 1),
	}

	return substr
}

type sendSubscribeStream struct {
	stream transport.Stream

	config *SubscribeConfig

	// resolvedStart is the absolute start group from SUBSCRIBE_OK.
	resolvedStart GroupSequence
	okReceived    bool

	// endGroup is the last group that may be delivered, from SUBSCRIBE_END.
	endGroup GroupSequence
	ended    bool

	mu sync.Mutex

	droppedCh chan struct{}
	drops     []SubscribeDrop

	id SubscribeID
}

// readSubscribeResponses consumes SUBSCRIBE_END and SUBSCRIBE_DROP messages
// from the publisher until the stream ends.
func (substr *sendSubscribeStream) readSubscribeResponses() {
	for {
		resp, err := readSubscribeResponse(substr.stream)
		if err != nil {
			return
		}

		switch {
		case resp.ok != nil:
			// SUBSCRIBE_OK carries a plain absolute sequence.
			substr.setResolvedStart(GroupSequence(resp.ok.Group))
		case resp.end != nil:
			substr.setEnd(GroupSequence(resp.end.Group))
		case resp.drop != nil:
			substr.appendDrop(SubscribeDrop{
				StartGroup: GroupSequence(resp.drop.GroupStart),
				EndGroup:   GroupSequence(resp.drop.GroupEnd),
				ErrorCode:  SubscribeErrorCode(resp.drop.ErrorCode),
			})
		}
	}
}

// subscribeResponse holds one decoded publisher message from the Subscribe
// Stream; exactly one field is non-nil.
type subscribeResponse struct {
	ok   *message.SubscribeOkMessage
	end  *message.SubscribeEndMessage
	drop *message.SubscribeDropMessage
}

func readSubscribeResponse(stream io.Reader) (subscribeResponse, error) {
	head := make([]byte, 1)
	if _, err := io.ReadFull(stream, head); err != nil {
		return subscribeResponse{}, err
	}

	msgType, _, err := message.ReadVarint(head)
	if err != nil {
		return subscribeResponse{}, err
	}

	switch msgType {
	case message.MessageTypeSubscribeOk:
		var msg message.SubscribeOkMessage
		if err := msg.Decode(stream); err != nil {
			return subscribeResponse{}, err
		}
		return subscribeResponse{ok: &msg}, nil
	case message.MessageTypeSubscribeEnd:
		var msg message.SubscribeEndMessage
		if err := msg.Decode(stream); err != nil {
			return subscribeResponse{}, err
		}
		return subscribeResponse{end: &msg}, nil
	case message.MessageTypeSubscribeDrop:
		var msg message.SubscribeDropMessage
		if err := msg.Decode(stream); err != nil {
			return subscribeResponse{}, err
		}
		return subscribeResponse{drop: &msg}, nil
	default:
		return subscribeResponse{}, fmt.Errorf("unexpected SUBSCRIBE response type: %d", msgType)
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

func (substr *sendSubscribeStream) setResolvedStart(seq GroupSequence) {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	substr.resolvedStart = seq
	substr.okReceived = true
}

func (substr *sendSubscribeStream) setEnd(seq GroupSequence) {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	substr.endGroup = seq
	substr.ended = true
}

func (substr *sendSubscribeStream) updateSubscribe(newConfig *SubscribeConfig) error {
	if newConfig == nil {
		// TODO: Handle nil config case if necessary (e.g., return an error, ignore the update, etc.)
		return nil
	}

	// Send the message first before updating config
	ordered := boolToWireFlag(newConfig.Ordered)

	groupStart := groupSequenceToWire(newConfig.StartGroup)

	groupEnd := groupSequenceToWire(newConfig.EndGroup)

	sum := message.SubscribeUpdateMessage{
		SubscriberPriority:   uint8(newConfig.Priority),
		SubscriberOrdered:    ordered,
		SubscriberMaxLatency: newConfig.MaxLatency,
		GroupStart:           groupStart,
		GroupEnd:             groupEnd,
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
