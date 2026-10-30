package moqt

import (
	"bytes"
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
	endSent         bool
	// redirected is set by writeRedirect: the stream is over, and close and
	// closeWithError have nothing left to do.
	redirected bool
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
		StartGroup: groupSequenceFromWire(updateMsg.GroupStart),
		EndGroup:   groupSequenceFromWire(updateMsg.GroupEnd),
	}

	substr.mu.Lock()
	substr.config = config
	substr.mu.Unlock()

	return config, nil
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

// writeRedirect sends SUBSCRIBE_REDIRECT as the subscription's only answer
// and ends the stream. It fails once SUBSCRIBE_OK or SUBSCRIBE_END went out.
func (substr *receiveSubscribeStream) writeRedirect(m message.SubscribeRedirectMessage) error {
	substr.mu.Lock()
	defer substr.mu.Unlock()

	if substr.responseStarted || substr.endSent {
		return errSubscriptionAnswered
	}

	// One write, so the type is never sent without the body.
	var buf bytes.Buffer
	buf.WriteByte(byte(message.MessageTypeSubscribeRedirect))
	if err := m.Encode(&buf); err != nil {
		return err
	}

	// Nothing may follow, even when the write fails partway: no SUBSCRIBE_OK
	// from a group opened later, and no SUBSCRIBE_END when the writer closes.
	substr.responseStarted = true
	substr.endSent = true
	substr.redirected = true

	if _, err := substr.stream.Write(buf.Bytes()); err != nil {
		// Part of it may be out; reset the stream rather than leave it torn.
		cancelStreamWithError(substr.stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return err
	}
	_ = substr.stream.Close() // not actionable: the redirect is out, and the stream ends either way
	// The subscriber has nothing more to say: stop reading, so the stream
	// completes and its stream credit returns.
	substr.stream.CancelRead(transport.StreamErrorCode(SubscribeErrorCodeInternal))
	return nil
}

// isRedirected reports whether writeRedirect has ended the stream.
func (substr *receiveSubscribeStream) isRedirected() bool {
	substr.mu.Lock()
	defer substr.mu.Unlock()
	return substr.redirected
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

func (substr *receiveSubscribeStream) close() error {
	if substr.isRedirected() {
		return nil
	}
	return substr.stream.Close()
}

func (substr *receiveSubscribeStream) closeWithError(code SubscribeErrorCode) error {
	if substr.isRedirected() {
		return nil
	}
	cancelStreamWithError(substr.stream, transport.StreamErrorCode(code))
	return nil
}
