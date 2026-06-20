package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// This file adds a SATURATING, unpaced real-QUIC egress benchmark — the harness
// every other I/O benchmark in this package is NOT.
//
// Why this exists (the measured gap):
//   - BenchmarkGroupWriter_WriteFrame writes to io.Discard (no-op Write) and so
//     measures only the encode glue; its own comment notes the ~4.8M MB/s it
//     reports is fake. It cannot see QUIC write cost.
//   - BenchmarkStreamIo_RealQUIC and BenchmarkBroadcastServer_HighLoad reuse a
//     publisher paced at ~30fps via a time.Ticker, so the transport is nearly
//     idle (~300 frames/sec) and absolute throughput is publisher-limited, not
//     egress-limited.
//
// BenchmarkEgress_Saturating streams frames back-to-back with NO ticker. QUIC
// flow control paces the publisher to the subscriber's drain rate, so ns/op is
// the true per-frame egress cost end-to-end (encode + QUIC framing + TLS +
// packetization + UDP syscall). CPU-profile it to rank where that cost lives.
//
//	framesPerGroup (fpg) isolates the cost:
//	  - 1   : one frame per group — OpenGroup overhead per frame (the broadcast
//	          harness's pattern; worst case).
//	  - 256 : many frames per group — amortizes OpenGroup; isolates WriteFrame.
//
//	Matrix: 1K{fpg-256}, 16K{fpg-1,fpg-256}, 64K{fpg-1,fpg-256}.
//
//	1K/fpg-1 (one uni stream per frame at high rate) is covered by
//	TestOpenGroup_BackpressuresOnStreamLimit, which exercises the uni-stream-limit
//	path that OpenGroup now backpressures via OpenUniStreamSync. It's omitted here
//	because -benchtime=1x (the integration job) reads too few groups to reach the
//	stream limit.
//
// This is a real-QUIC integration benchmark (like BenchmarkStreamIo_RealQUIC),
// so it is excluded from the PR microbenchmark run and measured in the
// benchmark-integration job (-benchtime=1x) instead.
//
// Profile:
//
//	go test -run '^$' -bench 'BenchmarkEgress_Saturating/payload-1K/fpg-256$' \
//	    -benchtime=5s -count=10 -cpuprofile=cpu.out -memprofile=mem.out ./moqt/

func BenchmarkEgress_Saturating(b *testing.B) {
	if testing.Short() {
		b.Skip("real-QUIC integration benchmark; skipped in -short")
	}
	configs := []struct{ size, fpg int }{
		{1 << 10, 256},
		{16 << 10, 1}, {16 << 10, 256},
		{64 << 10, 1}, {64 << 10, 256},
	}

	for _, c := range configs {
		size, fpg := c.size, c.fpg
		b.Run(fmt.Sprintf("payload-%s/fpg-%d", humanSize(size), fpg), func(b *testing.B) {
			ctx := b.Context()

			// The publisher streams from setup (no gate); QUIC flow control
			// paces it to the subscriber's drain rate, so it stalls until the
			// subscriber connects and the timed loop sees steady-state egress.
			srv, addr := setupSaturatingServer(b, ctx, size, fpg)
			defer closeBroadcastServer(b, srv)

			sess, tr := dialAndSubscribe(b, ctx, addr)
			defer sess.CloseWithError(NoError, "done")
			defer tr.Close()

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

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

// setupSaturatingServer starts a real QUIC/WebTransport server whose publisher
// streams frames back-to-back (NO ticker), writing framesPerGroup frames per
// group. It reuses the proven TLS/quic/server wiring from
// broadcast_benchmark_test.go verbatim; only the publisher pacing differs.
// It takes testing.TB so both benchmarks (*testing.B) and tests (*testing.T)
// can reuse it.
func setupSaturatingServer(tb testing.TB, ctx context.Context, frameSize, framesPerGroup int) (*Server, string) {
	tb.Helper()

	// Grab a free UDP port for the QUIC listener.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("failed to find available port: %v", err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	server := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{NextProtoH3, NextProtoMOQ},
			Certificates:       []tls.Certificate{generateTestCert(tb)},
			InsecureSkipVerify: true,
		},
		QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	}

	// Publish the broadcast track on DefaultMux. Unpaced: no ticker.
	PublishFunc(ctx, "/server.broadcast", func(tw *TrackWriter) {
		frame := NewFrame(frameSize)
		data := make([]byte, frameSize)
		for i := range data {
			data[i] = byte(i % 256)
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			gw, err := tw.OpenGroup(ctx)
			if err != nil {
				return
			}
			for i := 0; i < framesPerGroup; i++ {
				frame.Reset()
				frame.Write(data)
				if err := gw.WriteFrame(frame); err != nil {
					gw.CancelWrite(InternalGroupErrorCode)
					return
				}
			}
			if err := gw.Close(); err != nil {
				return
			}
		}
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server error: %v", err)
		}
	}()

	return server, "https://" + addr + "/broadcast"
}

// dialAndSubscribe connects a single client and subscribes to the broadcast
// track, retrying the dial until the asynchronously-bound listener is ready
// (same pattern as stream_io_benchmark_test.go). testing.TB so tests and
// benchmarks can share it.
func dialAndSubscribe(tb testing.TB, ctx context.Context, addr string) (*Session, *TrackReader) {
	tb.Helper()

	client := Dialer{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	ready, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	var sess *Session
	for {
		s, derr := client.Dial(ready, addr, nil)
		if derr == nil {
			sess = s
			cancelWait()
			break
		}
		if ready.Err() != nil {
			cancelWait()
			tb.Fatalf("dial %s: %v (listener not ready or upgrade failed)", addr, derr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	annRecv, err := sess.AcceptAnnounce("/")
	if err != nil {
		tb.Fatalf("accept announce: %v", err)
	}
	defer annRecv.Close()

	ann, err := annRecv.ReceiveAnnouncement(ctx)
	if err != nil {
		tb.Fatalf("receive announcement: %v", err)
	}
	if !ann.IsActive() {
		tb.Fatal("announcement not active")
	}

	tr, err := sess.Subscribe(ctx, ann.BroadcastPath(), TrackName("index"), nil)
	if err != nil {
		tb.Fatalf("subscribe: %v", err)
	}
	return sess, tr
}
