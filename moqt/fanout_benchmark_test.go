package moqt

// This file holds the system-level FAN-OUT benchmark PAIR, which together isolate
// the two scaling axes of relay fan-out. Both are real-QUIC integration benches
// (loopback, self-signed TLS, unpaced publisher): excluded from -short and measured
// in the integration job, mirroring BenchmarkEgress_Saturating. The publisher(s)
// start ONCE and the subscribers dial ONCE and stay; b.N scales FRAMES DELIVERED
// (aggregate), not connection cycles.
//
// TWO REGIMES — keep them distinct in name and reading; they answer different
// questions:
//
//   - ViewerConnections (1 track -> N viewer connections): each "sub" is an
//     INDEPENDENT QUIC connection (one Dial = one viewer), all subscribing to the
//     SAME track. This is the broadcast/live model: each viewer pays full
//     per-connection transport, so the wire carries N copies of the payload and the
//     ceiling is the unicast UDP send rate. ViewerConnectionsLatency is the latency
//     lens on this regime.
//
//   - TracksPerConnection (1 connection -> N tracks): ONE QUIC connection
//     subscribes to N DISTINCT tracks, sharing one congestion controller / ACK path
//     / packet-header amortization. This isolates per-TRACK cost (per-stream,
//     per-frame) from per-CONNECTION cost, and reports cross-track fairness WITHIN
//     that shared congestion-control domain (see reportReaderFairness).
//
// Comparing the two at the same N answers the central architectural question: is the
// fan-out cost per-connection (QUIC connection overhead) or per-track (stream/frame
// work)? If TracksPerConnection throughput stays well above ViewerConnections at the
// same N, the dominant cost is per-connection.
//
// Why system-level, not another microbenchmark: BenchmarkBroadcastServer_HighLoad
// reconnects every client every b.N iteration, so it measures connection
// SETUP/TEARDOWN churn, not steady fan-out. BenchmarkEgress_Saturating is 1->1.
// Broadcast.ServeTrack (broadcast.go) invokes the published handler ONCE PER
// subscriber, so encode is O(N) in recipients — there is no "encode once, copy to N
// streams" path.

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

// BenchmarkFanOut_ViewerConnections measures the VIEWER-CONNECTIONS fan-out regime:
// 1 published track -> N INDEPENDENT viewer QUIC connections (the broadcast/live
// model). Each "sub" is one Dial = one connection = one viewer, all subscribing to
// the SAME track. It reuses setupSaturatingServer (fpg-256 amortizes OpenGroup),
// then holds N viewers for the whole run and reads until b.N frames are delivered
// in aggregate.
//
// Reported MB/s (via b.SetBytes) is AGGREGATE; custom metrics add per-viewer
// throughput and footprint. The viewer-count sweep is the point: a flat per-viewer
// MB/s as N grows means fan-out is cheap; a collapsing one is the unicast-transport
// ceiling (each viewer = its own connection = its own copy of the bytes on the
// wire). Compare against BenchmarkFanOut_TracksPerConnection at the same N to split
// per-connection from per-track cost.
func BenchmarkFanOut_ViewerConnections(b *testing.B) {
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

// BenchmarkFanOut_ViewerConnectionsLatency is the latency lens on the
// ViewerConnections regime. It stamps an 8-byte big-endian MONOTONIC clock reading
// (time.Since(epoch) — QPC-backed on Windows, NOT wall-clock UnixNano, which ticks
// coarsely on Windows and quantizes sub-ms latency to 0) into the first 8 bytes of
// every frame so each viewer measures one-way publish->deliver latency. The
// publisher is GATED: after responding to the SUBSCRIBE via WriteInfo, it blocks
// until all viewers are parked in AcceptGroup, so no backlog built during dialing
// contaminates the latency samples. Reports p50/p95/p99/max plus the spread of
// per-viewer medians ("fairness"), which surfaces head-of-line blocking and
// scheduler unfairness that aggregate throughput hides. Small 1K payload at fpg-256
// maximizes frame rate, the regime where latency tails and GC-driven p99 matter
// most.
//
// Clock caveat: precision is bounded by time.Now() resolution on the HOST, not the
// wire — e.g. some Windows timer configs resolve time.Now() only to ~0.6ms, so
// sub-resolution latency quantizes toward 0 (frames read in the same tick as their
// stamp report ~0). Under load where queueing pushes latency past the tick,
// p50/p95/p99 and fairness are fully meaningful; on Linux CI time.Now() is sub-us,
// so even low-load p50 is meaningful there. A p50 near 0 therefore reads as "below
// clock resolution = effectively instant", not as a defect.
func BenchmarkFanOut_ViewerConnectionsLatency(b *testing.B) {
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

// BenchmarkFanOut_TracksPerConnection is the COMPLEMENT to
// BenchmarkFanOut_ViewerConnections: it dials ONE QUIC connection and subscribes to
// N DISTINCT tracks over that single session, sweeping N. This isolates the OTHER
// scaling axis — track-level cost when transport resources (one connection: one
// congestion controller, one ACK path, one set of packet headers) are SHARED across
// tracks — vs the viewer-connections regime where each copy pays full
// per-connection transport. Same 16K/fpg-256 as ViewerConnections for a direct
// same-N comparison. If TracksPerConnection throughput stays well above
// ViewerConnections at the same N, the dominant fan-out cost is per-connection; if
// they track together, it is per-track (per-stream/per-frame).
//
// Beyond aggregate throughput, this benchmark also reports cross-track throughput
// FAIRNESS (per-reader min/median/mean/p95/p99/max, Jain's fairness index, and a
// starved-reader count) — advisory metrics that begin characterizing fairness
// WITHIN the shared congestion-control domain, the data a future aggregate-vs-shard
// relay decision would need. See reportReaderFairness.
func BenchmarkFanOut_TracksPerConnection(b *testing.B) {
	if testing.Short() {
		b.Skip("real-QUIC integration benchmark; skipped in -short")
	}
	const (
		frameSize = 16 << 10 // match ViewerConnections for direct comparison
		fpg       = 256
	)
	// The publishers are GATED (see setupMultiTrackServer): they WriteInfo to flush
	// each SUBSCRIBE_OK, then block until all N tracks are subscribed before
	// blasting. Without the gate, already-blasting publishers starve later
	// subscribes' OKs (the subscribe handshake timed out at ~21 tracks ungated);
	// gating fixes that and measures STEADY-STATE track fan-out. tracks-64 is held
	// back: its STEADY-STATE delivery is fair and fast (Jain 1.000, ~5300 fps
	// aggregate measured directly), but its first-run ns/op carries ~10-12s of
	// variable overhead whose source is UNIDENTIFIED: setup, read-loop,
	// session-teardown, AND server.Close all measure fast (server.Close was timed
	// directly at <=0.7ms for 64 tracks in a focused test — exonerated). It is NOT
	// a production server.Close problem; the measurement is polluted, not the
	// delivery.
	for _, numTracks := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("tracks-%d", numTracks), func(b *testing.B) {
			ctx := b.Context()
			ready := make(chan struct{})
			srv, addr := setupMultiTrackServer(b, ctx, frameSize, fpg, numTracks, ready)
			defer closeBroadcastServer(b, srv)
			readTracksPerConnection(b, addr, numTracks, frameSize, ready)
		})
	}
}

// readFanout is the VIEWER-CONNECTIONS regime: it dials numSubs INDEPENDENT QUIC
// connections (one viewer each), each subscribing to the SAME single track, then
// delegates to fanoutReaders. Each "sub" is one Dial = one connection = one viewer.
func readFanout(b *testing.B, addr string, numSubs, frameSize int, latency bool, epoch time.Time, release chan struct{}) {
	ctx := b.Context()
	subs := make([]*TrackReader, numSubs)
	sessions := make([]*Session, numSubs)
	for i := range numSubs {
		s, tr := dialAndSubscribe(b, ctx, addr)
		sessions[i] = s
		subs[i] = tr
	}
	fanoutReaders(b, subs, frameSize, latency, epoch, release, func() {
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
	})
}

// readTracksPerConnection is the TRACKS-PER-CONNECTION regime: it dials ONE QUIC
// connection and subscribes to numTracks DISTINCT tracks over that single session
// (AcceptAnnounce + ReceiveAnnouncement x N + Subscribe x N), then delegates to
// fanoutReaders. Each "track" is one Subscribe on the shared connection — the
// contrast axis to readFanout's per-viewer connections. ready is the publisher
// gate, passed through to fanoutReaders as its release (closed once readers are
// parked) so all N subscribes complete before any publisher blasts.
func readTracksPerConnection(b *testing.B, addr string, numTracks, frameSize int, ready chan struct{}) {
	ctx := b.Context()

	client := Dialer{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}
	// Retry dial until the asynchronously-bound listener is ready (same pattern as
	// dialAndSubscribe).
	dialCtx, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	var sess *Session
	for {
		s, derr := client.Dial(dialCtx, addr, nil)
		if derr == nil {
			sess = s
			cancelWait()
			break
		}
		if dialCtx.Err() != nil {
			cancelWait()
			b.Fatalf("dial %s: %v (listener not ready or upgrade failed)", addr, derr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	annRecv, err := sess.AcceptAnnounce("/")
	if err != nil {
		b.Fatalf("accept announce: %v", err)
	}
	// Subscribe to numTracks distinct tracks over the ONE session.
	tracks := make([]*TrackReader, numTracks)
	for i := range numTracks {
		ann, err := annRecv.ReceiveAnnouncement(ctx)
		if err != nil {
			b.Fatalf("receive announcement %d: %v", i, err)
		}
		tr, err := sess.Subscribe(ctx, ann.BroadcastPath(), TrackName("index"), nil)
		if err != nil {
			b.Fatalf("subscribe %d: %v", i, err)
		}
		tracks[i] = tr
	}

	fanoutReaders(b, tracks, frameSize, false /* latency */, time.Time{}, ready, func() {
		_ = annRecv.Close()
		for _, tr := range tracks {
			if tr != nil {
				_ = tr.Close()
			}
		}
		_ = sess.CloseWithError(NoError, "done")
	})
}

// fanoutReaders is the shared measurement core for both fan-out regimes: it runs
// one goroutine per TrackReader, reads until b.N frames are delivered in aggregate,
// then reports throughput, footprint, and (optionally) latency percentiles. The
// caller supplies the readers (obtained however the regime dictates) and a teardown
// that closes them and their session(s). For the latency bench release is a gate the
// caller closes once readers are parked; nil means no gating (throughput benches).
// epoch is the shared monotonic reference for stamping (unused when latency is
// false). Setup is excluded by b.ResetTimer; teardown by b.StopTimer.
func fanoutReaders(b *testing.B, readers []*TrackReader, frameSize int, latency bool, epoch time.Time, release chan struct{}, teardown func()) {
	numReaders := len(readers)

	b.SetBytes(int64(frameSize))
	b.ReportAllocs()

	target := int64(b.N)
	var framesReceived atomic.Int64

	// Per-reader latency collectors. Each is single-writer (its reader goroutine),
	// so the write into samples[i] is race-free; the atomic idx publishes a
	// consistent count that the driver reads after wg.Wait().
	var collectors []*latencyCollector
	if latency {
		collectors = make([]*latencyCollector, numReaders)
	}

	// Per-reader frame counters drive the cross-sectional throughput-fairness
	// metrics. Each is single-writer (its reader goroutine); read after wg.Wait().
	readerFrames := make([]atomic.Int64, numReaders)

	runCtx, cancel := context.WithCancel(b.Context())
	defer cancel()
	if teardown != nil {
		defer teardown()
	}

	// Launch readers BEFORE starting the timer / releasing the gated publisher.
	var wg sync.WaitGroup
	wg.Add(numReaders)
	for i, tr := range readers {
		var col *latencyCollector
		if latency {
			col = newLatencyCollector(fanoutLatencyCap)
			collectors[i] = col
		}
		go func(tr *TrackReader, col *latencyCollector, rf *atomic.Int64) {
			defer wg.Done()
			frame := NewFrame(frameSize)
			// Readers self-terminate at the aggregate target, so there is no
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
					rf.Add(1)
					if col != nil {
						if body := frame.Body(); len(body) >= 8 {
							stamp := int64(binary.BigEndian.Uint64(body[:8]))
							// Monotonic: time.Since uses the same clock the
							// publisher stamped with (QPC-backed, not wall-clock
							// UnixNano). Resolution is still bounded by the host
							// time.Now() tick — see BenchmarkFanOut_ViewerConnectionsLatency.
							col.add(time.Since(epoch) - time.Duration(stamp))
						}
					}
				}
			}
		}(tr, col, &readerFrames[i])
	}

	// Stall watchdog: cancel instead of hanging if delivery stalls. The check is
	// RATE-based, not just zero-progress: a trickle (a few frames/window) would
	// never trip a ==last check and the run would report garbage at a trickle
	// rate. Track the best per-window progress; cancel when a window collapses to
	// <10% of best (covers zero progress too). The first window establishes the
	// baseline so cold-start ramp doesn't false-trip.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var best int64
		warmed := false
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
				delta := cur - last
				last = cur
				if delta > best {
					best = delta
					warmed = true
					continue
				}
				if !warmed || delta*10 < best {
					b.Logf("fanout: delivery stalled at %d/%d frames (window=%d best=%d); cancelling", cur, target, delta, best)
					cancel()
					return
				}
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
		b.ReportMetric(float64(delivered)/float64(numReaders)/elapsed.Seconds(), "frames/sec/reader")
	}
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.ReportMetric(float64(m.HeapInuse)/1024/1024, "heap-MB")

	if latency {
		reportFanoutLatency(b, collectors)
	}
	if elapsed > 0 {
		reportReaderFairness(b, readerFrames, elapsed, numReaders)
	}
}

// reportFanoutLatency sorts the merged per-reader samples and reports the
// p50/p95/p99/max one-way latencies plus the spread between the fastest and slowest
// reader medians (fairness). Fractional microseconds keep a sub-us loopback median
// from flooring to 0; latencyAt clamps any stray non-positive sample to 0
// defensively (monotonic stamps should never be negative).
func reportFanoutLatency(b *testing.B, collectors []*latencyCollector) {
	b.Helper()
	// Fractional microseconds so a sub-microsecond loopback median does not floor
	// to 0 (integer time.Duration.Microseconds would). Latencies are clamped to
	// >= 0 inside latencyAt, so clock skew never produces negative reported values.
	asMicros := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000.0 }

	var all []time.Duration
	perReaderMedians := make([]float64, 0, len(collectors))
	for _, c := range collectors {
		s := c.collected()
		if len(s) == 0 {
			continue
		}
		slices.Sort(s)
		all = append(all, s...)
		perReaderMedians = append(perReaderMedians, asMicros(latencyAt(s, 0.50)))
	}
	if len(all) == 0 {
		return
	}
	slices.Sort(all)
	b.ReportMetric(asMicros(latencyAt(all, 0.50)), "p50-us")
	b.ReportMetric(asMicros(latencyAt(all, 0.95)), "p95-us")
	b.ReportMetric(asMicros(latencyAt(all, 0.99)), "p99-us")
	b.ReportMetric(asMicros(latencyAt(all, 1.0)), "max-us")
	if len(perReaderMedians) > 1 {
		slices.Sort(perReaderMedians)
		b.ReportMetric(perReaderMedians[len(perReaderMedians)-1]-perReaderMedians[0], "fairness-spread-us")
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

// reportReaderFairness reports the cross-sectional distribution of per-reader
// throughput plus a fairness summary. Each reader's throughput is its total frames
// delivered over the whole run divided by elapsed (a view ACROSS readers at one
// point, not an intra-run time-series). For BenchmarkFanOut_TracksPerConnection
// this is fairness across tracks sharing ONE congestion-control domain — the
// quantity a future aggregate-vs-shard relay decision would need; for
// ViewerConnections it is fairness across independent viewer connections. Jain's
// fairness index is 1.0 when all readers are equal and tends to 1/N as one reader
// dominates. "starved-readers" counts readers below half the median throughput
// (advisory threshold). Percentiles are coarse at small N (clamped to the available
// samples). All fairness metrics are advisory/non-gating.
func reportReaderFairness(b *testing.B, readerFrames []atomic.Int64, elapsed time.Duration, numReaders int) {
	b.Helper()
	if numReaders <= 0 || elapsed <= 0 {
		return
	}
	secs := elapsed.Seconds()
	fps := make([]float64, numReaders)
	for i := range numReaders {
		fps[i] = float64(readerFrames[i].Load()) / secs
	}
	sorted := slices.Clone(fps)
	slices.Sort(sorted)

	var sum, sumSq float64
	for _, v := range fps {
		sum += v
		sumSq += v * v
	}
	mean := sum / float64(numReaders)
	median := quantileF64(sorted, 0.50)

	b.ReportMetric(sorted[0], "reader-fps-min")
	b.ReportMetric(median, "reader-fps-median")
	b.ReportMetric(mean, "reader-fps-mean")
	b.ReportMetric(quantileF64(sorted, 0.95), "reader-fps-p95")
	b.ReportMetric(quantileF64(sorted, 0.99), "reader-fps-p99")
	b.ReportMetric(sorted[len(sorted)-1], "reader-fps-max")

	// Jain's fairness index over per-reader throughput: 1.0 = perfectly fair.
	var jain float64
	if sumSq > 0 {
		jain = (sum * sum) / (float64(numReaders) * sumSq)
	}
	b.ReportMetric(jain, "jain-fairness")

	// Advisory starvation: readers below half the median throughput.
	threshold := median * 0.5
	var starved int
	for _, v := range fps {
		if v < threshold {
			starved++
		}
	}
	b.ReportMetric(float64(starved), "starved-readers")
}

// quantileF64 returns the p-quantile (0..1) of an already-sorted float64 slice.
func quantileF64(sorted []float64, p float64) float64 {
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
	return sorted[idx]
}

// fanoutLatencyCap bounds per-reader latency memory. 16K samples/reader is far more
// than p99 needs and keeps a 64-reader run under ~8 MB.
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
// blocks until ready is closed, so viewers connect and park in AcceptGroup before
// steady frames flow (no dialing-phase backlog) — and (b) stamps an 8-byte
// big-endian MONOTONIC clock reading (time.Since(epoch)) into the first 8 bytes of
// every frame. epoch is shared in-process with the viewers; time.Since uses Go's
// monotonic clock (QPC-backed on Windows) rather than wall-clock UnixNano. NOTE: on
// some Windows timer configs time.Now() itself only resolves to ~0.6ms, so even
// monotonic latency quantizes toward 0 — see the clock caveat on
// BenchmarkFanOut_ViewerConnectionsLatency. Like setupSaturatingServer the publisher
// is UNPACED once released; QUIC flow control paces it to the slowest viewer.
// frameSize must be >= 8.
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
		// Respond to the SUBSCRIBE explicitly with WriteInfo so the viewer's
		// Subscribe() handshake completes during dialing, BEFORE the gate blocks.
		// (OpenGroup would send this OK lazily via ensureInfo, but WriteInfo avoids
		// opening a throwaway uni stream + group header just to flush the response.)
		if err := tw.WriteInfo(PublishInfo{}); err != nil {
			return
		}

		// Gate: block until all viewers are connected and reading, so no backlog
		// builds during dialing. A closed ready is a no-op receive.
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

// setupMultiTrackServer starts a real QUIC/WebTransport server that publishes
// numTracks DISTINCT tracks on one TrackMux (paths /server.t0 .. /server.t{N-1}),
// each with an UNPACED publisher (no ticker) like setupSaturatingServer. QUIC flow
// control paces each to its subscriber's drain rate. A single client subscribes to
// all N tracks over ONE connection (see readTracksPerConnection) — the
// tracks-per-connection regime. The server config mirrors setupSaturatingServer
// verbatim; only the per-track publish loop differs. Each publisher is GATED on
// ready: it WriteInfos to flush the SUBSCRIBE_OK, then blocks until ready is
// closed (by fanoutReaders, once readers are parked) before blasting. Without the
// gate, already-blasting publishers starve later subscribes' OKs and the handshake
// times out (~21 tracks ungated); with it the sweep measures steady-state fan-out.
func setupMultiTrackServer(tb testing.TB, ctx context.Context, frameSize, framesPerGroup, numTracks int, ready <-chan struct{}) (*Server, string) {
	tb.Helper()

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

	for t := range numTracks {
		path := fmt.Sprintf("/server.t%d", t)
		mux.PublishFunc(ctx, BroadcastPath(path), func(tw *TrackWriter) {
			// Flush the SUBSCRIBE_OK (sent lazily on first OpenGroup via
			// ensureInfo) with WriteInfo so the subscribe handshake completes
			// during setup, BEFORE the gate blocks production.
			if err := tw.WriteInfo(PublishInfo{}); err != nil {
				return
			}
			// Gate: block until all tracks are subscribed (ready closed by
			// fanoutReaders once readers are parked). Without this, already-
			// blasting publishers starve later subscribes' OKs.
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
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server error: %v", err)
		}
	}()

	return server, "https://" + addr + "/moqt"
}
