package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const migrationTrackPath = BroadcastPath("/mobile/audio")

// A subscriber moves back and forth between two networks, as a phone
// between Wi-Fi and cellular, and keeps receiving its track on the same
// session, over native QUIC and WebTransport alike.
func TestSession_AddPath_Moves(t *testing.T) {
	tests := map[string]struct {
		scheme        string
		cutBeforeMove bool
	}{
		"native QUIC, old network still up":     {scheme: "moqt"},
		"native QUIC, old network already gone": {scheme: "moqt", cutBeforeMove: true},
		"WebTransport, old network still up":    {scheme: "https"},
		"WebTransport, old network gone":        {scheme: "https", cutBeforeMove: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.scheme == "https" && raceEnabled {
				// quic-go replaces a connection's socket on a path switch
				// without the lock ConnectionState reads it under, and its
				// HTTP/3 layer, under WebTransport, calls ConnectionState.
				t.Skip("quic-go's path switch races with ConnectionState")
			}
			if tt.cutBeforeMove {
				skipDeadNetworkMove(t)
			}
			sub := subscribeMigrating(t, tt.scheme)
			sub.requireGroups(t)
			wifi, wifiLink := sub.addLink(t)
			cell, cellLink := sub.addLink(t)
			sub.moveTo(t, wifi)
			sub.requireGroupsOn(t, wifiLink, "first move")
			cur, curLink, next, nextLink := wifi, wifiLink, cell, cellLink

			for move := 1; move <= 4; move++ {
				nextLink.down.Store(false)
				if tt.cutBeforeMove {
					curLink.down.Store(true)
				}

				sub.moveTo(t, next)
				// Make before break: the old network goes only once the new
				// path carries data. Cut sooner, the client's answer to the
				// server's PATH_CHALLENGE can go out on the old path and be
				// lost (quic-go#5408), which a v0.63 server never asks again.
				sub.requireGroupsOn(t, nextLink, "move "+strconv.Itoa(move))
				curLink.down.Store(true)

				sub.requireGroupsOn(t, nextLink, "move "+strconv.Itoa(move))
				cur, curLink, next, nextLink = next, nextLink, cur, curLink
			}
			assert.NoError(t, sub.sess.Context().Err(), "the session should survive every move")
		})
	}
}

// Closing the active path, whose network is gone, moves the session onto a
// socket the OS routes, and groups keep arriving.
func TestPath_Close_Active(t *testing.T) {
	sub := subscribeMigrating(t, "moqt")
	sub.requireGroups(t)
	p, link := sub.addLink(t)
	sub.moveTo(t, p)
	sub.requireGroupsOn(t, link, "on the added path")
	link.down.Store(true) // its network is gone

	require.NoError(t, p.Close())

	before := sub.groups.Load()
	assert.Eventually(t, func() bool { return sub.groups.Load() >= before+20 }, 5*time.Second, 10*time.Millisecond,
		"groups should keep arriving over the socket the session was dialed from")
	assert.Eventually(t, link.closed.Load, 5*time.Second, 10*time.Millisecond, "the closed path's socket should be closed")
}

func TestPath_Switch_NotValidated(t *testing.T) {
	sub := subscribeMigrating(t, "moqt")
	p, _ := sub.addLink(t)

	err := p.Switch()

	assert.ErrorIs(t, err, ErrPathNotValidated)
}

func TestSession_AddPath_Unsupported(t *testing.T) {
	tests := map[string]struct {
		conn StreamConn
	}{
		// A connection from DialQUICFunc, WebSocket, or a server's: none
		// carries a migrator.
		"connection without a migrator": {conn: &FakeStreamConn{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sess := newSession(tt.conn, NewTrackMux(0), nil, nil, nil, nil, nil, nil, sessionSetup{}, nil)
			t.Cleanup(func() { _ = sess.CloseWithError(NoError, "test done") }) // not actionable: cleanup
			link := &fakeLink{PacketConn: listenLoopback(t)}

			p, err := sess.AddPath(link)

			assert.ErrorIs(t, err, ErrMigrationUnsupported)
			assert.Nil(t, p)
			assert.True(t, link.closed.Load(), "a socket no path takes should be closed")
		})
	}
}

// skipDeadNetworkMove skips a move off a network already gone. The client
// answers the server's PATH_CHALLENGE on the path it is leaving
// (quic-go#5408), so the move needs a server that challenges the new path
// again, as quic-go's does from v0.64 (quic-go#5930); a v0.63 server, which
// these tests run, may never follow the client.
func skipDeadNetworkMove(tb testing.TB) {
	tb.Helper()
	tb.Skip("a move off a network already gone needs a quic-go v0.64+ server")
}

// moveTimeout bounds a move in these tests.
const moveTimeout = 5 * time.Second

// migratingSub is a session subscribed to a track published every 20 ms.
type migratingSub struct {
	sess   *Session
	groups atomic.Int64 // groups received
}

func subscribeMigrating(tb testing.TB, scheme string) *migratingSub {
	tb.Helper()
	addr := freePort(tb)
	mux := NewTrackMux(0)
	mux.PublishFunc(tb.Context(), migrationTrackPath, publishEvery20ms)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(tb)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 10 * time.Second},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server: %v", err)
		}
	}()
	tb.Cleanup(func() { closeServer(tb, srv) })

	dialer := &Dialer{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only self-signed cert
		// The keep-alive tells the server of a move when nothing else would:
		// the first move off the dialed socket, from a network already gone.
		QUICConfig: &quic.Config{KeepAlivePeriod: time.Second, MaxIdleTimeout: 10 * time.Second},
	}
	ctx, cancel := context.WithTimeout(tb.Context(), 10*time.Second)
	defer cancel()
	var sess *Session
	require.Eventually(tb, func() bool {
		var err error
		sess, err = dialer.Dial(ctx, scheme+"://"+addr+"/", nil)
		return err == nil
	}, 10*time.Second, 50*time.Millisecond)
	tb.Cleanup(func() { _ = sess.CloseWithError(NoError, "test done") }) // not actionable: cleanup
	tr, err := sess.Subscribe(ctx, migrationTrackPath, "audio", nil)
	require.NoError(tb, err)
	sub := &migratingSub{sess: sess}
	// The reader ends with the session, closed by the cleanup above.
	go func() {
		frame := NewFrame(64)
		for {
			gr, err := tr.AcceptGroup(sess.Context())
			if err != nil {
				return
			}
			for range gr.Frames(frame) {
			}
			sub.groups.Add(1)
		}
	}()
	return sub
}

func publishEvery20ms(tw *TrackWriter) {
	ctx := tw.Context()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	frame := NewFrame(64)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		gw, err := tw.OpenGroup(ctx)
		if err != nil {
			return
		}
		frame.Reset()
		_, _ = frame.Write(make([]byte, 64)) // not actionable: a frame buffer write cannot fail
		if err := gw.WriteFrame(frame); err != nil {
			gw.CancelWrite(InternalGroupErrorCode)
			return
		}
		_ = gw.Close() // not actionable: a group the peer refuses is its loss
	}
}

func (s *migratingSub) addLink(tb testing.TB) (*Path, *fakeLink) {
	tb.Helper()
	link := &fakeLink{PacketConn: listenLoopback(tb)}
	p, err := s.sess.AddPath(link)
	require.NoError(tb, err)
	return p, link
}

func (s *migratingSub) moveTo(tb testing.TB, p *Path) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), moveTimeout)
	defer cancel()
	require.NoError(tb, p.Probe(ctx))
	require.NoError(tb, p.Switch())
}

// requireGroupsOn waits until the server sends over link and groups keep
// arriving: groups already queued in the client could arrive without a move,
// datagrams on link cannot.
func (s *migratingSub) requireGroupsOn(tb testing.TB, link *fakeLink, msg string) {
	tb.Helper()
	reads, groups := link.reads.Load(), s.groups.Load()
	require.Eventually(tb, func() bool {
		return link.reads.Load()-reads >= 12 && s.groups.Load()-groups >= 10
	}, moveTimeout, 5*time.Millisecond, msg)
}

// requireGroups waits until groups arrive.
func (s *migratingSub) requireGroups(tb testing.TB) {
	tb.Helper()
	groups := s.groups.Load()
	require.Eventually(tb, func() bool { return s.groups.Load()-groups >= 5 }, 3*time.Second, 5*time.Millisecond)
}

func listenLoopback(tb testing.TB) net.PacketConn {
	tb.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conn.Close() }) // not actionable: cleanup
	return conn
}
