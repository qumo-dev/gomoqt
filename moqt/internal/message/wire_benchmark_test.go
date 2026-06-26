package message_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

// maxUint62 is the largest value encodable in a QUIC varint (62-bit).
const maxUint62 uint64 = 1<<62 - 1

// sinkWireBytes keeps a returned slice live so the compiler cannot
// dead-code-eliminate the WriteMessageLength call in BenchmarkWriteMessageLength.
var sinkWireBytes []byte

// encodeMessage encodes m into a fresh byte slice via its Encode method.
func encodeMessage(m interface{ Encode(io.Writer) error }) []byte {
	var buf bytes.Buffer
	if err := m.Encode(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// tile replicates b to fill a ~64 KiB buffer so a decode reader rarely drains
// during a run. The size is a CONSTANT: unlike bytes.Repeat(b, b.N+1) it does
// not scale with the iteration count, so it cannot OOM a slow runner or inflate
// GC pressure as b.N grows (the trap #213 fixed in BenchmarkFrame_Decode). On a
// rare drain the decode benches rewind with reader.Reset(repeating), which is
// zero-allocation, so drains never pollute allocs/op and add only negligible
// overhead relative to a full decode.
func tile(b []byte) []byte {
	const budget = 1 << 16 // 64 KiB
	return bytes.Repeat(b, budget/len(b)+1)
}

// The *_Encode benchmarks below write to io.Discard rather than a *bytes.Buffer.
// A bytes.Buffer grows its internal slice on Write, and that growth is counted by
// -benchmem — previously inflating the reported Encode cost to ~3 allocs/op when
// Encode's own allocation is only 1 scratch buffer. io.Discard isolates Encode's
// true allocation. (The encodeMessage helper above still uses bytes.Buffer because
// it needs the encoded bytes back for the *_Decode benchmarks.)

// --- GroupMessage ---

func BenchmarkGroupMessage_Encode(b *testing.B) {
	msg := message.GroupMessage{
		SubscribeID:   1 << 20,
		GroupSequence: 1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGroupMessage_Decode(b *testing.B) {
	msg := message.GroupMessage{
		SubscribeID:   1 << 20,
		GroupSequence: 1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.GroupMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.GroupMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- SubscribeMessage ---

func BenchmarkSubscribeMessage_Encode(b *testing.B) {
	msg := message.SubscribeMessage{
		SubscribeID:          1 << 28,
		BroadcastPath:        "relay.example.com/eu-west/cluster-a",
		TrackName:            "video/track/segment-0001",
		SubscriberPriority:   128,
		SubscriberOrdered:    1,
		SubscriberMaxLatency: 1 << 21,
		StartGroup:           1 << 20,
		EndGroup:             1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSubscribeMessage_Decode(b *testing.B) {
	msg := message.SubscribeMessage{
		SubscribeID:          1 << 28,
		BroadcastPath:        "relay.example.com/eu-west/cluster-a",
		TrackName:            "video/track/segment-0001",
		SubscriberPriority:   128,
		SubscriberOrdered:    1,
		SubscriberMaxLatency: 1 << 21,
		StartGroup:           1 << 20,
		EndGroup:             1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.SubscribeMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.SubscribeMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- AnnounceMessage ---

func BenchmarkAnnounceMessage_Encode(b *testing.B) {
	msg := message.AnnounceMessage{
		AnnounceStatus:      message.ACTIVE,
		BroadcastPathSuffix: "eu-west/cluster-a/publisher-42",
		HopIDs: []uint64{
			1 << 10,
			1 << 16,
			1 << 21,
			1 << 28,
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnnounceMessage_Decode(b *testing.B) {
	msg := message.AnnounceMessage{
		AnnounceStatus:      message.ACTIVE,
		BroadcastPathSuffix: "eu-west/cluster-a/publisher-42",
		HopIDs: []uint64{
			1 << 10,
			1 << 16,
			1 << 21,
			1 << 28,
		},
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.AnnounceMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.AnnounceMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- AnnounceInterestMessage ---

func BenchmarkAnnounceInterestMessage_Encode(b *testing.B) {
	msg := message.AnnounceInterestMessage{
		BroadcastPathPrefix: "eu-west/cluster-a",
		ExcludeHop:          1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnnounceInterestMessage_Decode(b *testing.B) {
	msg := message.AnnounceInterestMessage{
		BroadcastPathPrefix: "eu-west/cluster-a",
		ExcludeHop:          1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.AnnounceInterestMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.AnnounceInterestMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- FetchMessage ---

func BenchmarkFetchMessage_Encode(b *testing.B) {
	msg := message.FetchMessage{
		BroadcastPath: "relay.example.com/eu-west/cluster-a",
		TrackName:     "video/track/keyframe-0001",
		Priority:      200,
		GroupSequence: 1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFetchMessage_Decode(b *testing.B) {
	msg := message.FetchMessage{
		BroadcastPath: "relay.example.com/eu-west/cluster-a",
		TrackName:     "video/track/keyframe-0001",
		Priority:      200,
		GroupSequence: 1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.FetchMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.FetchMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- SubscribeOkMessage ---

func BenchmarkSubscribeOkMessage_Encode(b *testing.B) {
	msg := message.SubscribeOkMessage{
		PublisherPriority:   64,
		PublisherOrdered:    1,
		PublisherMaxLatency: 1 << 21,
		StartGroup:          1 << 20,
		EndGroup:            1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSubscribeOkMessage_Decode(b *testing.B) {
	msg := message.SubscribeOkMessage{
		PublisherPriority:   64,
		PublisherOrdered:    1,
		PublisherMaxLatency: 1 << 21,
		StartGroup:          1 << 20,
		EndGroup:            1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.SubscribeOkMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.SubscribeOkMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- SubscribeUpdateMessage ---

func BenchmarkSubscribeUpdateMessage_Encode(b *testing.B) {
	msg := message.SubscribeUpdateMessage{
		SubscriberPriority:   128,
		SubscriberOrdered:    1,
		SubscriberMaxLatency: 1 << 21,
		StartGroup:           1 << 20,
		EndGroup:             1 << 28,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSubscribeUpdateMessage_Decode(b *testing.B) {
	msg := message.SubscribeUpdateMessage{
		SubscriberPriority:   128,
		SubscriberOrdered:    1,
		SubscriberMaxLatency: 1 << 21,
		StartGroup:           1 << 20,
		EndGroup:             1 << 28,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.SubscribeUpdateMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.SubscribeUpdateMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- SubscribeDropMessage ---

func BenchmarkSubscribeDropMessage_Encode(b *testing.B) {
	msg := message.SubscribeDropMessage{
		StartGroup: 1 << 20,
		EndGroup:   1 << 28,
		ErrorCode:  1 << 10,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSubscribeDropMessage_Decode(b *testing.B) {
	msg := message.SubscribeDropMessage{
		StartGroup: 1 << 20,
		EndGroup:   1 << 28,
		ErrorCode:  1 << 10,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.SubscribeDropMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.SubscribeDropMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- GoawayMessage ---

func BenchmarkGoawayMessage_Encode(b *testing.B) {
	msg := message.GoawayMessage{
		NewSessionURI: "https://relay.example.com/eu-west/moqt-session?token=abc123",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGoawayMessage_Decode(b *testing.B) {
	msg := message.GoawayMessage{
		NewSessionURI: "https://relay.example.com/eu-west/moqt-session?token=abc123",
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.GoawayMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.GoawayMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- ProbeMessage ---

func BenchmarkProbeMessage_Encode(b *testing.B) {
	msg := message.ProbeMessage{
		Bitrate: 1 << 24, // ~16 Mbps
		RTT:     1 << 10, // ~1000 ms
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProbeMessage_Decode(b *testing.B) {
	msg := message.ProbeMessage{
		Bitrate: 1 << 24,
		RTT:     1 << 10,
	}
	encoded := encodeMessage(msg)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	var decoded message.ProbeMessage

	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		decoded = message.ProbeMessage{}
		if err := decoded.Decode(reader); err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}

// --- Varint boundary benchmarks ---
//
// Covers the four QUIC varint length buckets (1/2/4/8 bytes) plus the max
// valid uint62 value. These are the boundaries that any optimization of
// WriteMessageLength / ReadMessageLength must respect.

var varintMagnitudes = []struct {
	name string
	val  uint64
}{
	{"0", 0},
	{"127", 127},             // maxVarInt1 (1-byte)
	{"16383", 16383},         // maxVarInt2 (2-byte)
	{"1<<21", 1 << 21},       // inside 4-byte bucket
	{"1<<28", 1 << 28},       // inside 4-byte bucket
	{"maxUint62", maxUint62}, // maxVarInt8 (8-byte, max valid)
}

func BenchmarkWriteMessageLength(b *testing.B) {
	for _, tc := range varintMagnitudes {
		b.Run(tc.name, func(b *testing.B) {
			// Determine the encoded width purely for SetBytes.
			n := 0
			switch {
			case tc.val <= 127:
				n = 1
			case tc.val <= 16383:
				n = 2
			case tc.val <= (1<<32 - 1):
				n = 4
			default:
				n = 8
			}
			// Reuse one constant-capacity destination across iterations (reset
			// to [:0]), mirroring the internal BenchmarkWriteVarint. The original
			// allocated a fresh make([]byte, 0, n) each iteration and attributed
			// that harness slice to WriteMessageLength; reusing the destination
			// isolates the function's own cost. The capacity is a fixed 8
			// (covers the widest varint) rather than exactly n: an exact-cap
			// destination perturbs escape analysis for the 1-byte case under
			// this external test package and reports a phantom alloc.
			dst := make([]byte, 0, 8)

			b.ReportAllocs()
			b.SetBytes(int64(n))
			b.ResetTimer()

			for b.Loop() {
				out, _ := message.WriteMessageLength(dst[:0], tc.val)
				sinkWireBytes = out
			}
		})
	}
}

// BenchmarkReadMessageLength reads a message-length varint from a
// bytes.Reader of pre-encoded bytes. This isolates the hot path that the
// open optimization PRs target (~2 allocs/frame on main).
func BenchmarkReadMessageLength(b *testing.B) {
	for _, tc := range varintMagnitudes {
		b.Run(tc.name, func(b *testing.B) {
			// Pre-encode the varint once.
			n := 0
			switch {
			case tc.val <= 127:
				n = 1
			case tc.val <= 16383:
				n = 2
			case tc.val <= (1<<32 - 1):
				n = 4
			default:
				n = 8
			}
			encoded := make([]byte, 0, n)
			encoded, _ = message.WriteMessageLength(encoded, tc.val)
			// Tile the encoded varint so the reader never drains mid-bench.
			repeating := tile(encoded)
			reader := bytes.NewReader(repeating)

			b.ReportAllocs()
			b.SetBytes(int64(len(encoded)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := message.ReadMessageLength(reader)
				if err != nil {
					if err == io.EOF {
						reader.Reset(repeating)
						continue
					}
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkReadMessageLength_MaxUint62_NoBody isolates the DoS-relevant
// worst case: an untrusted peer advertises the largest legal message length
// (uint62 max, an 8-byte varint). ReadMessageLength itself only reads the
// varint header; the Decode methods then attempt to allocate a body of that
// size. This benchmark measures ONLY the header read so that future changes
// can be checked for both perf and validation behavior without actually
// materializing a multi-exabyte buffer.
//
// No validation logic is added here — measurement only.
func BenchmarkReadMessageLength_MaxUint62_NoBody(b *testing.B) {
	encoded := make([]byte, 0, 8)
	encoded, _ = message.WriteMessageLength(encoded, maxUint62)
	repeating := tile(encoded)
	reader := bytes.NewReader(repeating)

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := message.ReadMessageLength(reader)
		if err != nil {
			if err == io.EOF {
				reader.Reset(repeating)
				continue
			}
			b.Fatal(err)
		}
	}
}
