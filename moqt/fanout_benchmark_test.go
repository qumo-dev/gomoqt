package moqt

// This file holds the two FLAGSHIP system-level benchmarks from the system-suite
// design: steady-state fan-out and its latency lens. They fill the one gap none
// of the existing real-QUIC harnesses cover — many LONG-LIVED subscribers on a
// single published track — and answer the central production question the data
// plane is already characterized as NOT answering at the per-frame level.
//
// Why this is system-level, not another microbenchmark:
//
//   - BenchmarkBroadcastServer_HighLoad reconnects every client every b.N
//     iteration, so it measures connection SETUP/TEARDOWN churn, not steady
//     fan-out (an earlier 17%-decode memprofile artifact came from exactly this
//     churn). BenchmarkEgress_Saturating is 1 publisher -> 1 subscriber.
//   - Broadcast.ServeTrack (broadcast.go) invokes the published handler ONCE PER
//     SUBSCRIBER with a fresh *TrackWriter, so encode cost is O(N) in
//     subscribers — there is no "encode once, copy to N streams" path. These
//     benchmarks characterize that scaling: does per-subscriber throughput
//     degrade and CPU/subscriber rise as N grows?
//
// Both benchmarks are real-QUIC integration benches (loopback, self-signed TLS,
// unpaced publisher): excluded from -short and measured in the integration job,
// mirroring BenchmarkEgress_Saturating. The publisher is started ONCE and the
// subscribers dial ONCE and stay; b.N scales FRAMES DELIVERED (aggregate), not
// connection cycles — the Egress_Saturating discipline, inverted from HighLoad.

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// BenchmarkFanOut_SteadyState measures 1 unpaced publisher -> N LONG-LIVED
// subscribers over real loopback QUIC. It reuses setupSaturatingServer verbatim
// (fpg-256 amortizes OpenGroup), then holds N subscribers for the whole run and
// reads until b.N frames have been delivered in aggregate.
//
// Reported MB/s (via b.SetBytes) is AGGREGATE across all subscribers; the custom
// metrics add per-subscriber throughput and resource footprint. The subs-count
// sweep is the whole point: a flat per-subscriber MB/s as N grows means fan-out
// is cheap; a collapsing one is the O(N)-encode ceiling.
func BenchmarkFanOut_SteadyState(b *testing.B) {
	if testing.Short() {
		b.Skip("real-QUIC integration benchmark; skipped in -short")
	}
	const (
		frameSize = 16 << 10 // 16K: mid-size payload, isolates fan-out cost
		fpg       = 256      // many frames/group: amortizes OpenGroup
	)
	for _, numSubs := range []int{1, 4, 16, 64, 256} {
		b.Run(fmt.Sprintf("subs-%d", numSubs), func(b *testing.B) {
			ctx := b.Context()
			srv, addr := setupSaturatingServer(b, ctx, frameSize, fpg)
			defer closeBroadcastServer(b, srv)
			readFanout(b, addr, numSubs, frameSize, false /* latency */, time.Time{}, nil)
		})
	}
}

// BenchmarkFanOut_Latency stamps an 8-byte big-endian MONOTONIC clock reading
// (time.Since(epoch) — QPC-backed on Windows, NOT wall-clock UnixNano, which
// ticks coarsely on Windows and quantizes sub-ms latency to 0) into the first 8
// bytes of every frame so each subscriber can measure one-way publish->deliver
// latency. The publisher is GATED: after responding to the SUBSCRIBE via
// WriteInfo, it blocks until all subscribers are parked in AcceptGroup, so no
// backlog built during dialing contaminates the latency samples. Reports
// p50/p95/p99/max plus the spread of
// per-subscriber medians ("fairness"), which surfaces head-of-line blocking and
// scheduler unfairness that aggregate throughput hides. Small 1K payload at
// fpg-256 maximizes frame rate, the regime where latency tails and GC-driven
// p99 matter most.
//
// Clock caveat: precision is bounded by time.Now() resolution on the HOST, not
// the wire — e.g. some Windows timer configs resolve time.Now() only to ~0.6ms,
// so sub-resolution latency quantizes toward 0 (frames read in the same tick as
// their stamp report ~0). Under load where queueing pushes latency past the tick,
// p50/p95/p99 and fairness are fully meaningful; on Linux CI time.Now() is
// sub-us, so even low-load p50 is meaningful there. A p50 near 0 therefore reads
// as "below clock resolution = effectively instant", not as a defect.
func BenchmarkFanOut_Latency(b *testing.B) {
	if testing.Short() {
		b.Skip("real-QUIC integration benchmark; skipped in -short")
	}
	const (
		frameSize = 1 << 10 // 1K: high frame rate exercises the latency tail
		fpg       = 256
	)
	for _, numSubs := range []int{1, 16, 64} {
		b.Run(fmt.Sprintf("subs-%d", numSubs), func(b *testing.B) {
			ctx := b.Context()
			// epoch + ready are shared in-process: the publisher stamps
			// time.Since(epoch); readFanout closes ready once readers are parked.
			epoch := time.Now()
			ready := make(chan struct{})
			srv, addr := setupFanoutLatencyServer(b, ctx, frameSize, fpg, epoch, ready)
			defer closeBroadcastServer(b, srv)
			readFanout(b, addr, numSubs, frameSize, true /* latency */, epoch, ready)
		})
	}
}

// readFanout dials numSubs long-lived subscribers, runs each in its own goroutine
// reading until b.N frames are delivered in aggregate, then reports throughput,
// footprint, and (optionally) latency percentiles. For the latency bench the
// publisher is gated by release: readFanout launches the readers (they park in
// AcceptGroup), starts the timer, then closes release so production begins with
// readers already waiting — no dialing-phase backlog contaminates the samples.
// epoch is the shared monotonic reference for stamping (unused when latency is
// false). Setup is excluded by b.ResetTimer; teardown by b.StopTimer.
func readFanout(b *testing.B, addr string, numSubs, frameSize int, latency bool, epoch time.Time, release chan struct{}) {
	ctx := b.Context()

	// Dial all subscribers once and keep them for the whole timed run. Done
	// sequentially before the readers launch so the (expensive)
	// dial/announce/subscribe handshake is excluded from the measurement.
	subs := make([]*TrackReader, numSubs)
	sessions := make([]*Session, numSubs)
	for i := range numSubs {
		s, tr := dialAndSubscribe(b, ctx, addr)
		sessions[i] = s
		subs[i] = tr
	}
	defer func() {
		for _, tr := range subs {
			if tr != nil {
				_ = tr.Close()
			}
		}
		for _, s := range sessions {
			if s != nil {
				_ = s.CloseWithError(NoError, "done")
			}
		}
	}()

	b.SetBytes(int64(frameSize))
	b.ReportAllocs()

	target := int64(b.N)
	var framesReceived atomic.Int64

	// Per-subscriber latency collectors. Each is single-writer (its subscriber
	// goroutine), so the write into samples[i] is race-free; the atomic idx
	// publishes a consistent count that the driver reads after wg.Wait().
	var collectors []*latencyCollector
	if latency {
		collectors = make([]*latencyCollector, numSubs)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Launch readers BEFORE starting the timer / releasing the gated publisher.
	var wg sync.WaitGroup
	wg.Add(numSubs)
	for i, tr := range subs {
		var col *latencyCollector
		if latency {
			col = newLatencyCollector(fanoutLatencyCap)
			collectors[i] = col
		}
		go func(tr *TrackReader, col *latencyCollector) {
			defer wg.Done()
			frame := NewFrame(frameSize)
			// Subscribers self-terminate at the aggregate target, so there is no
			// detection overshoot: each checks framesReceived before every read.
			for framesReceived.Load() < target {
				gr, err := tr.AcceptGroup(runCtx)
				if err != nil {
					return
				}
				for framesReceived.Load() < target {
					if err := gr.ReadFrame(frame); err != nil {
						if err == io.EOF {
							break
						}
						return
					}
					framesReceived.Add(1)
					if col != nil {
						if body := frame.Body(); len(body) >= 8 {
							stamp := int64(binary.BigEndian.Uint64(body[:8]))
							// Monotonic: time.Since uses the same clock the
							// publisher stamped with (QPC-backed, not wall-clock
							// UnixNano). Resolution is still bounded by the host
							// time.Now() tick — see BenchmarkFanOut_Latency.
							col.add(time.Since(epoch) - time.Duration(stamp))
						}
					}
				}
			}
		}(tr, col)
	}

	// Stall watchdog: if delivery makes no progress for 2s (e.g. a subscriber
	// errored before reaching the target), cancel instead of hanging the run.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		last := framesReceived.Load()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				cur := framesReceived.Load()
				if cur >= target {
					return
				}
				if cur == last {
					b.Logf("fanout: delivery stalled at %d/%d frames; cancelling", cur, target)
					cancel()
					return
				}
				last = cur
			}
		}
	}()

	b.ResetTimer()
	start := time.Now()
	if release != nil {
		close(release) // gated publisher begins producing with readers parked
	}
	wg.Wait()
	elapsed := time.Since(start)
	b.StopTimer()

	delivered := framesReceived.Load()
	if elapsed > 0 {
		b.ReportMetric(float64(delivered)/elapsed.Seconds(), "frames/sec")
		b.ReportMetric(float64(delivered)/float64(numSubs)/elapsed.Seconds(), "frames/sec/sub")
	}
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.ReportMetric(float64(m.HeapInuse)/1024/1024, "heap-MB")

	if latency {
		reportFanoutLatency(b, collectors)
	}
}

// reportFanoutLatency sorts the merged per-subscriber samples and reports the
// p50/p95/p99/max one-way latencies plus the spread between the fastest and
// slowest subscriber medians (fairness). Fractional microseconds keep a sub-us
// loopback median from flooring to 0; latencyAt clamps any stray non-positive
// sample to 0 defensively (monotonic stamps should never be negative).
func reportFanoutLatency(b *testing.B, collectors []*latencyCollector) {
	b.Helper()
	// Fractional microseconds so a sub-microsecond loopback median does not floor
	// to 0 (integer time.Duration.Microseconds would). Latencies are clamped to
	// >= 0 inside latencyAt, so clock skew never produces negative reported values.
	asMicros := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000.0 }

	var all []time.Duration
	perSubMedians := make([]float64, 0, len(collectors))
	for _, c := range collectors {
		s := c.collected()
		if len(s) == 0 {
			continue
		}
		slices.Sort(s)
		all = append(all, s...)
		perSubMedians = append(perSubMedians, asMicros(latencyAt(s, 0.50)))
	}
	if len(all) == 0 {
		return
	}
	slices.Sort(all)
	b.ReportMetric(asMicros(latencyAt(all, 0.50)), "p50-us")
	b.ReportMetric(asMicros(latencyAt(all, 0.95)), "p95-us")
	b.ReportMetric(asMicros(latencyAt(all, 0.99)), "p99-us")
	b.ReportMetric(asMicros(latencyAt(all, 1.0)), "max-us")
	if len(perSubMedians) > 1 {
		slices.Sort(perSubMedians)
		b.ReportMetric(perSubMedians[len(perSubMedians)-1]-perSubMedians[0], "fairness-spread-us")
	}
}

// latencyAt returns the p-quantile (0..1) of an already-sorted sample slice.
func latencyAt(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	d := sorted[idx]
	if d < 0 {
		return 0
	}
	return d
}

// fanoutLatencyCap bounds per-subscriber latency memory. 16K samples/subscriber
// is far more than p99 needs and keeps a 64-subscriber run under ~8 MB.
const fanoutLatencyCap = 1 << 14

type latencyCollector struct {
	samples []time.Duration
	idx     atomic.Int64
}

func newLatencyCollector(capacity int) *latencyCollector {
	return &latencyCollector{samples: make([]time.Duration, capacity)}
}

func (c *latencyCollector) add(d time.Duration) {
	i := c.idx.Add(1) - 1
	if int(i) < len(c.samples) {
		c.samples[i] = d
	}
}

func (c *latencyCollector) collected() []time.Duration {
	n := int(c.idx.Load())
	if n > len(c.samples) {
		n = len(c.samples)
	}
	return c.samples[:n]
}

// setupFanoutLatencyServer mirrors setupSaturatingServer (egress_benchmark_test.go)
// but (a) GATES the publisher on ready — after responding to the SUBSCRIBE via
// WriteInfo (flushing the SUBSCRIBE_OK without opening a throwaway group), it
// blocks until ready is closed, so subscribers connect and park in AcceptGroup
// before steady frames flow (no dialing-phase backlog) —
// and (b) stamps an 8-byte big-endian MONOTONIC
// clock reading (time.Since(epoch)) into the first 8 bytes of every frame. epoch
// is shared in-process with the subscribers; time.Since uses Go's monotonic clock
// (QPC-backed on Windows) rather than wall-clock UnixNano. NOTE: on some Windows
// timer configs time.Now() itself only resolves to ~0.6ms, so even monotonic
// latency quantizes toward 0 — see the clock caveat on BenchmarkFanOut_Latency.
// Like setupSaturatingServer
// the publisher is UNPACED once released; QUIC flow control paces it to the
// slowest subscriber. frameSize must be >= 8.
func setupFanoutLatencyServer(tb testing.TB, ctx context.Context, frameSize, framesPerGroup int, epoch time.Time, ready <-chan struct{}) (*Server, string) {
	tb.Helper()
	if frameSize < 8 {
		tb.Fatalf("frameSize must be >= 8 for latency stamping, got %d", frameSize)
	}

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("failed to find available port: %v", err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	mux := NewTrackMux(0)

	server := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{NextProtoH3, NextProtoMOQ},
			Certificates:       []tls.Certificate{generateTestCert(tb)},
			InsecureSkipVerify: true,
		},
		QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	}

	mux.PublishFunc(ctx, "/server.broadcast", func(tw *TrackWriter) {
		// Respond to the SUBSCRIBE explicitly with WriteInfo so the subscriber's
		// Subscribe() handshake completes during dialing, BEFORE the gate blocks.
		// (OpenGroup would send this OK lazily via ensureInfo, but WriteInfo avoids
		// opening a throwaway uni stream + group header just to flush the response.)
		if err := tw.WriteInfo(PublishInfo{}); err != nil {
			return
		}

		// Gate: block until all subscribers are connected and reading, so no
		// backlog builds during dialing. A closed ready is a no-op receive.
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}

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
				binary.BigEndian.PutUint64(data[:8], uint64(int64(time.Since(epoch))))
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
