package moqt

import (
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

func newSendSubscribeStream(id SubscribeID, stream transport.Stream, initConfig *SubscribeConfig, dropHandler func(SubscribeDrop)) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		id:          id,
		config:      initConfig,
		stream:      stream,
		dropHandler: dropHandler,
	}

	return substr
}

func (substr *sendSubscribeStream) startResponseLoop() {
	go func() {
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
				substr.notifyDrop(SubscribeDrop{
					StartGroup: groupSequenceFromWire(drop.StartGroup),
					EndGroup:   groupSequenceFromWire(drop.EndGroup),
					ErrorCode:  SubscribeErrorCode(drop.ErrorCode),
				})
				return
			}
		}

	}()
}

type sendSubscribeStream struct {
	stream transport.Stream

	config *SubscribeConfig

	info PublishInfo

	mu     sync.Mutex
	dropMu sync.Mutex

	dropHandler func(SubscribeDrop)
	drop        SubscribeDrop
	dropSeen    bool
	dropSent    bool

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

func (substr *sendSubscribeStream) notifyDrop(drop SubscribeDrop) {
	substr.dropMu.Lock()
	if substr.dropSeen {
		substr.dropMu.Unlock()
		return
	}
	substr.dropSeen = true
	substr.drop = drop
	handler := substr.dropHandler
	shouldDeliver := handler != nil && !substr.dropSent
	if shouldDeliver {
		substr.dropSent = true
	}
	substr.dropMu.Unlock()

	if shouldDeliver {
		go handler(drop)
	}
}

func (substr *sendSubscribeStream) setDropHandler(handler func(SubscribeDrop)) {
	substr.dropMu.Lock()
	substr.dropHandler = handler
	drop := substr.drop
	shouldDeliver := substr.dropSeen && !substr.dropSent && handler != nil
	if shouldDeliver {
		substr.dropSent = true
	}
	substr.dropMu.Unlock()

	if shouldDeliver {
		go handler(drop)
	}
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
