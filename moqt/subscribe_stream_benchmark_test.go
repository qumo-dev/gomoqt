package moqt

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

// These benchmarks cover the SUBSCRIBE control-plane stream glue that wraps the
// per-message Encode/Decode (already benched in internal/message). They are
// control-plane (per subscribe/update, not per frame), so they guard against
// regressions and surface per-call allocations — notably the 1-byte type-prefix
// writes ([]byte{byte(msgType)}) and the 1-byte head read (make([]byte, 1)) —
// rather than chase data-plane throughput.

// discardStream returns a transport.Stream whose Write is a no-op success, so
// the encode path is measured with no sink allocation on the writer side.
func discardStream() *FakeQUICStream { return &FakeQUICStream{} }

// BenchmarkReceiveSubscribeStream_WriteInfo measures writeInfoLocked: it writes
// a 1-byte SUBSCRIBE_OK type prefix ([]byte{...} — one allocation) then encodes
// a SubscribeOkMessage to the stream. Runs once per subscribe response.
func BenchmarkReceiveSubscribeStream_WriteInfo(b *testing.B) {
	info := PublishInfo{
		Priority:   TrackPriority(5),
		MaxLatency: 2000,
	}
	substr := &receiveSubscribeStream{stream: discardStream()}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := substr.writeInfoLocked(info); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReceiveSubscribeStream_WriteDrop measures writeDrop on the path
// where the info response was already sent (responseStarted): it writes a
// 1-byte SUBSCRIBE_DROP type prefix then encodes a SubscribeDropMessage. Runs
// once per dropped range.
func BenchmarkReceiveSubscribeStream_WriteDrop(b *testing.B) {
	drop := SubscribeDrop{StartGroup: 1, EndGroup: 10, ErrorCode: 0}
	substr := &receiveSubscribeStream{
		stream:          discardStream(),
		responseStarted: true,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := substr.writeDrop(drop); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSendSubscribeStream_UpdateSubscribe measures updateSubscribe, which
// builds a SubscribeUpdateMessage and encodes it to the stream. Runs once per
// subscriber update.
func BenchmarkSendSubscribeStream_UpdateSubscribe(b *testing.B) {
	cfg := &SubscribeConfig{
		Priority:   TrackPriority(3),
		Ordered:    true,
		MaxLatency: 1000,
	}
	substr := &sendSubscribeStream{stream: discardStream()}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := substr.updateSubscribe(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadSubscribeResponse measures readSubscribeResponse, the subscriber
// side: it allocates a 1-byte head (make([]byte, 1)), reads it, varint-decodes
// the type, then decodes the SubscribeOk/SubscribeDrop body. The response bytes
// are built exactly as the writer produces them, so the reader accepts them.
// loopReader yields them forever with no allocation and no EOF.
func BenchmarkReadSubscribeResponse(b *testing.B) {
	var okBuf bytes.Buffer
	okBuf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
	_ = (message.SubscribeOkMessage{
		PublisherPriority:   5,
		PublisherMaxLatency: 2000,
	}).Encode(&okBuf)

	var dropBuf bytes.Buffer
	dropBuf.Write([]byte{byte(message.MessageTypeSubscribeDrop)})
	_ = (message.SubscribeDropMessage{
		StartGroup: 1,
		EndGroup:   10,
		ErrorCode:  0,
	}).Encode(&dropBuf)

	b.Run("subscribe-ok", func(b *testing.B) {
		reader := &loopReader{src: okBuf.Bytes()}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, _, err := readSubscribeResponse(reader); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("subscribe-drop", func(b *testing.B) {
		reader := &loopReader{src: dropBuf.Bytes()}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, _, err := readSubscribeResponse(reader); err != nil {
				b.Fatal(err)
			}
		}
	})
}
