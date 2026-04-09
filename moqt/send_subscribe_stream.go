package moqt

import (
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

func newSendSubscribeStream(id SubscribeID, stream Stream, initConfig *SubscribeConfig, info PublishInfo) *sendSubscribeStream {
	substr := &sendSubscribeStream{
		id:            id,
		config:        initConfig,
		stream:        stream,
		info:          info,
		wroteInfoChan: make(chan struct{}, 1),
		dropCh:        make(chan SubscribeDrop, 1),
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
					Ordered:    ok.PublisherOrdered != 0,
					MaxLatency: ok.PublisherMaxLatency,
					StartGroup: GroupSequence(ok.StartGroup),
					EndGroup:   GroupSequence(ok.EndGroup),
				})
				continue
			}

			if drop != nil {
				substr.notifyDrop()
				continue
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

	mu sync.Mutex

	dropCh chan SubscribeDrop

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
		substr.closeWithError(SubscribeErrorCodeInternal)
		return err
	}

	substr.mu.Lock()
	substr.config = newConfig
	substr.mu.Unlock()

	return nil
}

func (substr *sendSubscribeStream) notifyDrop() {

}

func (substr *sendSubscribeStream) ReadInfo() PublishInfo {
	return substr.info
}

func (substr *sendSubscribeStream) Dropped() <-chan SubscribeDrop {
	return substr.dropCh
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
