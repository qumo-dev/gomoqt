package moqt

import (
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

func newSendSubscribeStream(id SubscribeID, stream Stream, initConfig *SubscribeConfig, info PublishInfo, dropHandler func(SubscribeDrop)) *sendSubscribeStream {

	substr := &sendSubscribeStream{
		id:            id,
		config:        initConfig,
		stream:        stream,
		info:          info,
		wroteInfoChan: make(chan struct{}, 1),
		dropHandler:   dropHandler,
	}

	go func() {
		for {
			ok, drop, err := readSubscribeResponse(stream)
			if err != nil {
				// Handle error (e.g., log it, close the stream, etc.)
				return
			}

			if ok != nil {
				substr.updateInfo(PublishInfo{
					Priority:   TrackPriority(ok.PublisherPriority),
					Ordered:    boolFromWireFlag(ok.PublisherOrdered),
					MaxLatency: ok.PublisherMaxLatency,
					StartGroup: GroupSequence(ok.StartGroup),
					EndGroup:   GroupSequence(ok.EndGroup),
				})
				continue
			}

			if drop != nil {
				converted := SubscribeDrop{
					ErrorCode: SubscribeErrorCode(drop.ErrorCode),
				}
				converted.StartGroup = groupSequenceFromWire(drop.StartGroup)
				converted.EndGroup = groupSequenceFromWire(drop.EndGroup)
				substr.notifyDrop(converted)
				return
			}
		}

	}()

	return substr
}

type sendSubscribeStream struct {
	stream Stream

	config *SubscribeConfig

	info PublishInfo

	wroteInfoChan chan struct{}

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
	select {
	case substr.wroteInfoChan <- struct{}{}:
	default:
	}
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

	// Close the write side of the stream
	err := substr.stream.Close()
	// Do not cancel the read side on a graceful close: allow peer to finish sending

	return err
}

func (substr *sendSubscribeStream) closeWithError(code SubscribeErrorCode) {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	cancelStreamWithError(substr.stream, StreamErrorCode(code))
}
