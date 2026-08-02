package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

// These tests cover a multi-hop relay topology (hub -> N edges -> subscribers)
// using the lazy relay-handler pattern: an edge only subscribes to the hub once
// a local subscriber arrives for the announced track, and re-subscribes per
// subscriber wave. This mirrors how a production relay (e.g. qumo) wires
// TrackMux.Announce handlers, and is the scenario that matters for fan-out
// correctness at P>=3 edges.

const relayTestTrackPath = "/bench/carry"

// relayFixture is a hub publishing one track plus numEdges relay servers, each
// dialed to the hub and lazily relaying relayTestTrackPath to its own subscribers.
type relayFixture struct {
	hub   *Server
	edges []*relayEdge
}

type relayEdge struct {
	server *Server
	addr   string
}

// newRelayFixture starts a hub and numEdges relay edges, waiting for every edge
// to announce the track locally before returning. The returned cleanup function
// closes all servers; call it via defer.
func newRelayFixture(tb testing.TB, ctx context.Context, numEdges int) (*relayFixture, func()) {
	tb.Helper()

	cert := generateTestCert(tb)
	hubAddr := freePort(tb)
	hubMux := NewTrackMux(0)

	hub := &Server{
		Addr: hubAddr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{NextProtoMOQ},
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: true,
		},
		QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
		TrackMux:   hubMux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	}
	hubMux.PublishFunc(ctx, relayTestTrackPath, relayCarryHandler)

	go func() {
		if err := hub.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("hub: %v", err)
		}
	}()

	require.Eventually(tb, func() bool {
		sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+hubAddr, nil)
		if err != nil {
			return false
		}
		_ = sess.CloseWithError(NoError, "probe")
		return true
	}, 10*time.Second, 50*time.Millisecond)

	edges := make([]*relayEdge, numEdges)
	ready := make([]chan struct{}, numEdges)
	for i := range numEdges {
		edgeAddr := freePort(tb)
		edgeMux := NewTrackMux(0)
		edge := &Server{
			Addr: edgeAddr,
			TLSConfig: &tls.Config{
				NextProtos:         []string{NextProtoMOQ},
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: true,
			},
			QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
			TrackMux:   edgeMux,
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
		}
		go func() {
			if err := edge.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
				tb.Logf("edge server: %v", err)
			}
		}()
		edges[i] = &relayEdge{server: edge, addr: edgeAddr}
		ready[i] = make(chan struct{})

		go relayInstallLazyHandler(tb, ctx, hubAddr, edgeMux, ready[i])
	}

	for i, ch := range ready {
		select {
		case <-ch:
		case <-time.After(15 * time.Second):
			tb.Fatalf("edge%d: relay handler not installed in time", i)
		}
	}

	fixture := &relayFixture{hub: hub, edges: edges}
	cleanup := func() {
		closeServer(tb, hub)
		for _, e := range edges {
			closeServer(tb, e.server)
		}
	}
	return fixture, cleanup
}

// relayCarryHandler publishes a steady stream of 128-byte frames until ctx is done.
func relayCarryHandler(tw *TrackWriter) {
	ctx := tw.Context()
	_ = tw.WriteInfo(PublishInfo{})
	frame := NewFrame(128)
	data := make([]byte, 128)
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
		frame.Reset()
		_, _ = frame.Write(data)
		if err := gw.WriteFrame(frame); err != nil {
			gw.CancelWrite(InternalGroupErrorCode)
			return
		}
		_ = gw.Close()
	}
}

// relayInstallLazyHandler dials the hub, waits for the track announcement, and
// installs a TrackMux handler that subscribes to the hub only when a local
// subscriber arrives (the lazy relay pattern). ready is closed once installed.
func relayInstallLazyHandler(tb testing.TB, ctx context.Context, hubAddr string, edgeMux *TrackMux, ready chan struct{}) {
	sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+hubAddr, edgeMux)
	if err != nil {
		tb.Logf("relay dial: %v", err)
		return
	}

	ar, err := sess.AcceptAnnounce("/")
	if err != nil {
		tb.Logf("relay accept announce: %v", err)
		return
	}
	ann, err := ar.ReceiveAnnouncement(ctx)
	if err != nil {
		tb.Logf("relay receive announcement: %v", err)
		return
	}

	edgeMux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {
		hubSub, err := sess.Subscribe(ctx, ann.BroadcastPath(), "index", nil)
		if err != nil {
			return
		}
		defer hubSub.Close()

		frame := NewFrame(128)
		innerCtx := tw.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-innerCtx.Done():
				return
			default:
			}
			gr, err := hubSub.AcceptGroup(ctx)
			if err != nil {
				return
			}
			gw, err := tw.OpenGroup(ctx)
			if err != nil {
				return
			}
			for {
				if err := gr.ReadFrame(frame); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					gw.CancelWrite(InternalGroupErrorCode)
					return
				}
				if err := gw.WriteFrame(frame); err != nil {
					gw.CancelWrite(InternalGroupErrorCode)
					return
				}
			}
			_ = gw.Close()
		}
	}))

	close(ready)

	for {
		if _, err := ar.ReceiveAnnouncement(sess.Context()); err != nil {
			return
		}
	}
}

// relaySubscribeAndReadOne dials addr, subscribes to the announced track, and
// reads a single frame, reporting success/failure without leaving the session
// open (unless keepAlive is true, in which case the caller owns the session).
func relaySubscribeAndReadOne(ctx context.Context, addr string, timeout time.Duration) error {
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(subCtx, "moqt://"+addr, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer sess.CloseWithError(NoError, "done")

	ar, err := sess.AcceptAnnounce("/")
	if err != nil {
		return fmt.Errorf("accept announce: %w", err)
	}
	defer ar.Close()

	ann, err := ar.ReceiveAnnouncement(subCtx)
	if err != nil {
		return fmt.Errorf("receive announcement: %w", err)
	}
	tr, err := sess.Subscribe(subCtx, ann.BroadcastPath(), "index", nil)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer tr.Close()

	gr, err := tr.AcceptGroup(subCtx)
	if err != nil {
		return fmt.Errorf("accept group: %w", err)
	}
	frame := NewFrame(128)
	if err := gr.ReadFrame(frame); err != nil {
		return fmt.Errorf("read frame: %w", err)
	}
	return nil
}

// TestRelay_LazyHandlerFanOut verifies that a hub -> N edge relay topology using
// the lazy (on-demand) subscribe pattern delivers frames to all subscribers
// across every edge, for edge counts 2 through 4. This is the scenario a
// production relay uses: TrackMux.Announce installs a handler that subscribes
// to the upstream hub only once a local subscriber shows up.
func TestRelay_LazyHandlerFanOut(t *testing.T) {
	for _, numEdges := range []int{2, 3, 4} {
		t.Run(fmt.Sprintf("edges=%d", numEdges), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			fixture, cleanup := newRelayFixture(t, ctx, numEdges)
			defer cleanup()

			const subscribersPerEdge = 5
			type result struct {
				edge int
				err  error
			}
			results := make(chan result, numEdges*subscribersPerEdge)
			var wg sync.WaitGroup
			for i, edge := range fixture.edges {
				for range subscribersPerEdge {
					wg.Add(1)
					go func(edgeIdx int, addr string) {
						defer wg.Done()
						results <- result{edge: edgeIdx, err: relaySubscribeAndReadOne(ctx, addr, 15*time.Second)}
					}(i, edge.addr)
				}
			}
			wg.Wait()
			close(results)

			okByEdge := make(map[int]int)
			for r := range results {
				if r.err != nil {
					t.Errorf("edge%d subscriber failed: %v", r.edge, r.err)
					continue
				}
				okByEdge[r.edge]++
			}
			for i := range numEdges {
				require.Equal(t, subscribersPerEdge, okByEdge[i], "edge%d: not all subscribers received a frame", i)
			}
		})
	}
}

// TestRelay_SubscriberDisconnectReconnect verifies that subscribers on one edge
// disconnecting does not leave the relay in a state where a later edge fails to
// serve a fresh wave of subscribers. This targets state-leak-on-disconnect bugs
// in the announcement/subscription lifecycle.
func TestRelay_SubscriberDisconnectReconnect(t *testing.T) {
	for _, numEdges := range []int{2, 3} {
		t.Run(fmt.Sprintf("edges=%d", numEdges), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			fixture, cleanup := newRelayFixture(t, ctx, numEdges)
			defer cleanup()

			const wave1Subscribers = 5
			// Wave 1: subscribe to edge0, read one frame, then disconnect all.
			wave1Sessions := make([]*Session, 0, wave1Subscribers)
			for range wave1Subscribers {
				subCtx, subCancel := context.WithTimeout(ctx, 15*time.Second)
				sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(subCtx, "moqt://"+fixture.edges[0].addr, nil)
				subCancel()
				require.NoError(t, err)
				wave1Sessions = append(wave1Sessions, sess)

				ar, err := sess.AcceptAnnounce("/")
				require.NoError(t, err)
				ann, err := ar.ReceiveAnnouncement(ctx)
				require.NoError(t, err)
				tr, err := sess.Subscribe(ctx, ann.BroadcastPath(), "index", nil)
				require.NoError(t, err)

				gr, err := tr.AcceptGroup(ctx)
				require.NoError(t, err)
				frame := NewFrame(128)
				require.NoError(t, gr.ReadFrame(frame))
				_ = ar.Close()
				_ = tr.Close()
			}
			for _, sess := range wave1Sessions {
				_ = sess.CloseWithError(NoError, "wave1 done")
			}

			// Wave 2: subscribe to the last edge after wave 1 disconnected.
			targetEdge := numEdges - 1
			const wave2Subscribers = 5
			results := make(chan error, wave2Subscribers)
			var wg sync.WaitGroup
			for range wave2Subscribers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results <- relaySubscribeAndReadOne(ctx, fixture.edges[targetEdge].addr, 20*time.Second)
				}()
			}
			wg.Wait()
			close(results)

			ok := 0
			for err := range results {
				if err != nil {
					t.Errorf("wave2 subscriber failed: %v", err)
					continue
				}
				ok++
			}
			require.Equal(t, wave2Subscribers, ok, "edge%d: not all wave2 subscribers received a frame after wave1 disconnected", targetEdge)
		})
	}
}

// TestRelay_DelayedEdgeConnections verifies fan-out still succeeds when edges
// join the hub with a delay between them, rather than all at once. This targets
// startup-ordering races in announcement propagation.
func TestRelay_DelayedEdgeConnections(t *testing.T) {
	for _, numEdges := range []int{2, 3} {
		t.Run(fmt.Sprintf("edges=%d", numEdges), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			cert := generateTestCert(t)
			hubAddr := freePort(t)
			hubMux := NewTrackMux(0)
			hub := &Server{
				Addr: hubAddr,
				TLSConfig: &tls.Config{
					NextProtos:         []string{NextProtoMOQ},
					Certificates:       []tls.Certificate{cert},
					InsecureSkipVerify: true,
				},
				QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
				TrackMux:   hubMux,
				Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
				Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
			}
			hubMux.PublishFunc(ctx, relayTestTrackPath, relayCarryHandler)
			go func() {
				if err := hub.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
					t.Logf("hub: %v", err)
				}
			}()
			defer closeServer(t, hub)

			require.Eventually(t, func() bool {
				sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+hubAddr, nil)
				if err != nil {
					return false
				}
				_ = sess.CloseWithError(NoError, "probe")
				return true
			}, 10*time.Second, 50*time.Millisecond)

			edges := make([]*relayEdge, numEdges)
			ready := make([]chan struct{}, numEdges)
			for i := range numEdges {
				if i > 0 {
					time.Sleep(2 * time.Second)
				}
				edgeAddr := freePort(t)
				edgeMux := NewTrackMux(0)
				edgeSrv := &Server{
					Addr: edgeAddr,
					TLSConfig: &tls.Config{
						NextProtos:         []string{NextProtoMOQ},
						Certificates:       []tls.Certificate{cert},
						InsecureSkipVerify: true,
					},
					QUICConfig: &quic.Config{Allow0RTT: true, EnableDatagrams: true},
					TrackMux:   edgeMux,
					Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
					Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
				}
				go func() {
					if err := edgeSrv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
						t.Logf("edge server: %v", err)
					}
				}()
				edges[i] = &relayEdge{server: edgeSrv, addr: edgeAddr}
				ready[i] = make(chan struct{})
				go relayInstallLazyHandler(t, ctx, hubAddr, edgeMux, ready[i])
			}
			defer func() {
				for _, e := range edges {
					closeServer(t, e.server)
				}
			}()

			for i, ch := range ready {
				select {
				case <-ch:
				case <-time.After(15 * time.Second):
					t.Fatalf("edge%d: relay handler not installed in time", i)
				}
			}

			const subscribersPerEdge = 5
			type result struct {
				edge int
				err  error
			}
			results := make(chan result, numEdges*subscribersPerEdge)
			var wg sync.WaitGroup
			for i, edge := range edges {
				for range subscribersPerEdge {
					wg.Add(1)
					go func(edgeIdx int, addr string) {
						defer wg.Done()
						results <- result{edge: edgeIdx, err: relaySubscribeAndReadOne(ctx, addr, 20*time.Second)}
					}(i, edge.addr)
				}
			}
			wg.Wait()
			close(results)

			okByEdge := make(map[int]int)
			for r := range results {
				if r.err != nil {
					t.Errorf("edge%d subscriber failed: %v", r.edge, r.err)
					continue
				}
				okByEdge[r.edge]++
			}
			for i := range numEdges {
				require.Equal(t, subscribersPerEdge, okByEdge[i], "edge%d: not all subscribers received a frame", i)
			}
		})
	}
}

func freePort(tb testing.TB) string {
	tb.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(tb, err)
	addr := pc.LocalAddr().String()
	_ = pc.Close()
	return addr
}

func closeServer(tb testing.TB, srv *Server) {
	tb.Helper()
	done := make(chan struct{})
	go func() {
		_ = srv.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		tb.Logf("server.Close() did not complete in 5s")
	}
}
