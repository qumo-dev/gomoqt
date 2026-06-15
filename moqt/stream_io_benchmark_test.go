package moqt

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

// This file adds REAL-stream and memcpy-backed throughput benchmarks plus
// write-count instrumentation. It deliberately does NOT change any production
// write/buffering behavior — it only adds measurement scaffolding.
//
// Background (why this exists):
// Every I/O benchmark except broadcast_benchmark_test.go writes to io.Discard,
// whose Write is a no-op. BenchmarkGroupWriter_WriteFrame/size-65536 on main
// reports ~4.8M MB/s because the Write under test does zero work. These
// benchmarks make the real Write cost visible.

// ---------------------------------------------------------------------------
// Real-QUIC single-stream throughput benchmark
// ---------------------------------------------------------------------------

// BenchmarkStreamIo_RealQUIC measures end-to-end throughput of frame writes
// over a real local QUIC connection, reusing the proven broadcast harness
// (setupBroadcastServerWithFrameSize, generateTestCert) from
// broadcast_benchmark_test.go. The server opens one fresh unidirectional QUIC
// stream per group and writes a frame; a single client subscribes and reads
// b.N frames. b.SetBytes is the frame payload size, so the reported MB/s
// reflects real stream Write cost (encode + QUIC framing + transport), not a
// no-op sink.
//
// CAVEAT: the reused harness paces the publisher at ~30fps via a ticker, so
// absolute MB/s here is publisher-paced, not transport-limited. It still
// exercises a REAL QUIC stream and is the right place to compare payload
// sizes. For unpaced Write-side throughput see BenchmarkWriteThroughput_Memcpy
// (memcpy-backed fallback).
//
// Note: ns/op and MB/s are PRELIMINARY — concurrent worktrees may perturb
// timing. Treat numbers as comparative across payload sizes within one run.
func BenchmarkStreamIo_RealQUIC(b *testing.B) {
	sizes := []int{1 << 10, 16 << 10, 64 << 10} // 1K 16K 64K (harness cap at ~30fps makes 256K/1M slow)

	for _, size := range sizes {
		b.Run(fmt.Sprintf("payload-%s", humanSize(size)), func(b *testing.B) {
			ctx := b.Context()

			// Reuse the proven server-setup helper from broadcast_benchmark_test.go.
			srv, addr := setupBroadcastServerWithFrameSize(b, ctx, size)
			defer closeBroadcastServer(b, srv)

			client := Dialer{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				TLSConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}

			// Dial once the listener is ready (it binds asynchronously in the
			// setup goroutine). The WebTransport path now works end-to-end (#179);
			// see TestBroadcastServer_PublishSubscribeRoundTrip for the validated flow.
			var sess *Session
			ready, cancelWait := context.WithTimeout(ctx, 5*time.Second)
			for {
				s, derr := client.Dial(ready, addr, nil)
				if derr == nil {
					sess = s
					cancelWait()
					break
				}
				if ready.Err() != nil {
					cancelWait()
					b.Fatalf("dial %s: %v (listener not ready or upgrade failed)", addr, derr)
				}
				time.Sleep(50 * time.Millisecond)
			}
			defer sess.CloseWithError(NoError, "done")

			annRecv, err := sess.AcceptAnnounce("/")
			if err != nil {
				b.Fatalf("accept announce: %v", err)
			}
			defer annRecv.Close()

			ann, err := annRecv.ReceiveAnnouncement(ctx)
			if err != nil {
				b.Fatalf("receive announcement: %v", err)
			}
			if !ann.IsActive() {
				b.Fatal("announcement not active")
			}

			tr, err := sess.Subscribe(ctx, ann.BroadcastPath(), TrackName("index"), nil)
			if err != nil {
				b.Fatalf("subscribe: %v", err)
			}
			defer tr.Close()

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			// Read b.N frames from the single subscribed stream.
			frame := NewFrame(size)
			read := 0
			for read < b.N {
				gr, err := tr.AcceptGroup(ctx)
				if err != nil {
					if ctx.Err() != nil {
						break
					}
					b.Fatalf("accept group: %v", err)
				}
				for {
					if err := gr.ReadFrame(frame); err != nil {
						if err == io.EOF {
							break
						}
						b.Fatalf("read frame: %v", err)
					}
					read++
					if read >= b.N {
						break
					}
				}
			}

			b.StopTimer()
		})
	}
}

// ---------------------------------------------------------------------------
// Memcpy-backed throughput + write-count instrumentation
// ---------------------------------------------------------------------------

// BenchmarkWriteThroughput_Memcpy measures GroupWriter.WriteFrame throughput
// against a REAL-memcpy sink (growing BytesWriterSink) instead of io.Discard.
// This is the documented FALLBACK path: it exercises the real encode + a real
// byte copy, but not QUIC framing/transport. It is far better than io.Discard
// (Write does real work) but is NOT real-QUIC throughput.
//
// The counting wrapper is interposed between the GroupWriter and the sink so
// that write-count per frame and the write-size histogram are reported as
// custom metrics. This characterizes the one-stream-per-group and
// two-header-writes hypotheses:
//
//   - group open = 2 separate Write calls (stream-type varint + group message),
//     observed here only when counting around OpenGroup; WriteFrame itself
//     performs exactly 1 Write per frame (varint length + payload together).
func BenchmarkWriteThroughput_Memcpy(b *testing.B) {
	sizes := []int{1 << 10, 16 << 10, 64 << 10, 256 << 10, 1 << 20} // 1K 16K 64K 256K 1M

	for _, size := range sizes {
		b.Run(fmt.Sprintf("payload-%s", humanSize(size)), func(b *testing.B) {
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i % 256)
			}

			sink := NewBytesWriterSink(size + 16)
			counting := NewCountingSendStream(&sinkWriterSendStream{w: sink})

			// NOTE: a new GroupWriter is created per iteration to mirror the
			// one-stream-per-group production behavior. Each OpenGroup would
			// allocate a fresh stream; here we model one group per iteration.
			gw := newGroupWriter(counting, GroupSequence(1), nil)

			frame := NewFrame(size)
			frame.Write(payload)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				sink.Reset()
				if err := gw.WriteFrame(frame); err != nil {
					b.Fatalf("WriteFrame: %v", err)
				}
			}

			b.StopTimer()

			snap := counting.Snapshot()
			// WriteFrame issues exactly one stream Write per frame (varint
			// length prefix + payload are concatenated in encode and written
			// in a single Write call). Report it as a custom metric.
			if b.N > 0 {
				b.ReportMetric(float64(snap.WriteCalls)/float64(b.N), "writes/frame")
				b.ReportMetric(float64(snap.BytesWritten)/float64(b.N), "bytes/write")
			}
		})
	}
}

// BenchmarkWriteCount_GroupHeader counts the Write calls performed while
// opening a group and writing a single frame, to characterize the
// "two-header-writes per group" hypothesis. It uses a counting wrapper around
// a FakeQUICSendStream and reproduces the EXACT OpenGroup write sequence from
// track_writer.go:
//
//  1. message.StreamTypeGroup.Encode(stream)   -> 1 Write (1 byte)
//  2. message.GroupMessage{...}.Encode(stream) -> 1 Write (varint length +
//     subscribe-id + group-seq, typically 3-5 bytes)
//  3. newGroupWriter(stream, ...)              -> stores stream, no writes
//  4. groupWriter.WriteFrame(frame)            -> 1 Write (varint length +
//     payload, single concatenated buffer)
//
// So one group containing one frame = 3 Write calls. The benchmark reports
// writes/group-header (steps 1+2) and writes/frame (step 4) separately so the
// two hypotheses can be read directly off the output.
func BenchmarkWriteCount_GroupHeader(b *testing.B) {
	sizes := []int{1 << 10, 16 << 10, 64 << 10}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("payload-%s", humanSize(size)), func(b *testing.B) {
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i % 256)
			}

			// Discard-backed fake so we isolate WRITE-COUNT, not throughput.
			fake := &FakeQUICSendStream{WriteFunc: io.Discard.Write}
			counting := NewCountingSendStream(fake)

			// Pre-build the frame once; payload is fixed across iterations.
			frame := NewFrame(size)
			frame.Write(payload)

			b.ReportAllocs()
			b.ResetTimer()

			var headerWrites, frameWrites int64
			for i := 0; i < b.N; i++ {
				counting.Reset()

				// Step 1 + 2: the two group-header writes that OpenGroup
				// performs on the freshly-opened uni stream.
				if err := message.StreamTypeGroup.Encode(counting); err != nil {
					b.Fatalf("encode stream type: %v", err)
				}
				if err := (message.GroupMessage{
					SubscribeID:   0,
					GroupSequence: uint64(i),
				}).Encode(counting); err != nil {
					b.Fatalf("encode group message: %v", err)
				}
				hw := counting.Snapshot().WriteCalls

				// Step 3 + 4: construct the group writer (no writes) and
				// write exactly one frame through it.
				gw := newGroupWriter(counting, GroupSequence(1), nil)
				if err := gw.WriteFrame(frame); err != nil {
					b.Fatalf("WriteFrame: %v", err)
				}
				fw := counting.Snapshot().WriteCalls - hw

				headerWrites += hw
				frameWrites += fw
			}

			b.StopTimer()

			if b.N > 0 {
				b.ReportMetric(float64(headerWrites)/float64(b.N), "writes/group-header")
				b.ReportMetric(float64(frameWrites)/float64(b.N), "writes/frame")
				snap := counting.Snapshot()
				b.ReportMetric(float64(snap.BytesWritten)/float64(b.N), "bytes/op")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sinkWriterSendStream adapts an io.Writer (the BytesWriterSink) to the
// transport.SendStream interface for the counting-wrapper benchmark. Only
// Write needs to do real work; the rest are inert stubs.
type sinkWriterSendStream struct {
	w io.Writer
}

func (s *sinkWriterSendStream) Write(p []byte) (int, error)           { return s.w.Write(p) }
func (s *sinkWriterSendStream) CancelWrite(transport.StreamErrorCode) {}
func (s *sinkWriterSendStream) SetWriteDeadline(time.Time) error      { return nil }
func (s *sinkWriterSendStream) Close() error                          { return nil }
func (s *sinkWriterSendStream) Context() context.Context              { return context.Background() }

// humanSize returns a short label (1K, 16K, 64K, 256K, 1M) for a byte size.
func humanSize(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%dM", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%dK", n>>10)
	default:
		return fmt.Sprintf("%d", n)
	}
}
