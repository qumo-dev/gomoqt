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

// Migration tests dial through a fakeNAT, over a transport the test owns,
// and add paths over loopback sockets the test can cut, as networks that
// come and go. Every socket is a fakeLink, so that the connection's sockets
// are all of one kind (see Session.AddPath).

const migrationTrackPath = BroadcastPath("/mobile/audio")

// A subscriber moves back and forth between two networks, as a phone
// between Wi-Fi and cellular, and keeps receiving its track on the same
// session, over native QUIC and WebTransport alike. Each move closes the
// path left, as quic-go needs to hand its connection ID back, and the next
// move to that network adds a path over its transport again.
func TestSession_AddPath_Moves(t *testing.T) {
	for _, scheme := range []string{"moqt", "https"} {
		t.Run(scheme, func(t *testing.T) {
			if scheme == "https" && raceEnabled {
				// quic-go replaces a connection's socket on a path switch
				// without the lock ConnectionState reads it under, and its
				// HTTP/3 layer, under WebTransport, calls ConnectionState.
				t.Skip("quic-go's path switch races with ConnectionState")
			}
			sub := subscribeMigrating(t, scheme)
			sub.requireGroups(t)
			cur, next := newNetwork(t), newNetwork(t)
			curPath := sub.moveTo(t, cur)
			sub.requireGroupsOn(t, cur.link, "first move")

			for move := 1; move <= 4; move++ {
				next.link.down.Store(false)

				nextPath := sub.moveTo(t, next)
				// Make before break: the old network goes only once the new
				// path carries data. Cut sooner, the client's answer to the
				// server's PATH_CHALLENGE can go out on the old path and be
				// lost (quic-go#5408), which a v0.63 server never asks again.
				sub.requireGroupsOn(t, next.link, "move "+strconv.Itoa(move))
				require.NoError(t, curPath.Close())
				cur.link.down.Store(true)

				sub.requireGroupsOn(t, next.link, "move "+strconv.Itoa(move))
				cur, next, curPath = next, cur, nextPath
			}
			assert.NoError(t, sub.sess.Context().Err(), "the session should survive every move")
		})
	}
}

// A subscriber dialed over its own transport, on a socket bound to no
// network, moves to a bound Wi-Fi path, and keeps the dialed transport
// ready by probing a path over it there. When Wi-Fi goes, after the OS has
// moved the dialed socket to cellular, so that the server sees it at an
// address it never validated, the subscriber switches back: the server
// validates and follows the new address, and the groups keep arriving.
//
// A probe made before the route change does not validate the new address;
// this shows end to end that the server does, rather than relying on
// quic-go's Switch not validating again.
func TestSession_AddPath_DialTransportAfterRouteChange(t *testing.T) {
	tests := map[string]struct {
		probeBeforeChange bool
	}{
		"probed before the route change": {probeBeforeChange: true},
		"probed after the route change":  {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sub := subscribeMigrating(t, "moqt")
			sub.requireGroups(t)
			wifi := newNetwork(t)
			sub.moveTo(t, wifi)
			sub.requireGroupsOn(t, wifi.link, "on Wi-Fi")
			home, err := sub.sess.AddPath(sub.dial.tr)
			require.NoError(t, err)
			if tt.probeBeforeChange {
				sub.probe(t, home)
			}

			wifi.link.down.Store(true)
			newAddr := sub.nat.remap(t, sub.dial.link.LocalAddr())
			start := time.Now()
			if !tt.probeBeforeChange {
				sub.probe(t, home)
			}
			require.NoError(t, home.Switch())

			sub.requireGroupsOn(t, sub.dial.link, "after the switch back")
			t.Logf("groups resumed %s after the switch", time.Since(start).Round(time.Millisecond))
			assert.Eventually(t, func() bool { return sub.server(t).RemoteAddr().String() == newAddr.String() },
				moveTimeout, 10*time.Millisecond, "the server should follow the new address")
		})
	}
}

// On the transport it was dialed over, a session follows the OS's default
// network by itself: when the OS moves the socket to another network, the
// server follows, with nothing for the application to do.
func TestSession_PassiveMigration(t *testing.T) {
	sub := subscribeMigrating(t, "moqt")
	sub.requireGroups(t)

	newAddr := sub.nat.remap(t, sub.dial.link.LocalAddr())

	sub.requireGroupsOn(t, sub.dial.link, "after the route change")
	assert.Eventually(t, func() bool { return sub.server(t).RemoteAddr().String() == newAddr.String() },
		moveTimeout, 10*time.Millisecond)
}

// Dialer.Transport is what the session is dialed over.
func TestDialer_Transport(t *testing.T) {
	for _, scheme := range []string{"moqt", "https"} {
		t.Run(scheme, func(t *testing.T) {
			sub := subscribeMigrating(t, scheme)

			assert.Equal(t, sub.dial.link.LocalAddr().String(), sub.sess.LocalAddr().String())
		})
	}
}

func TestSession_AddPath_Unsupported(t *testing.T) {
	// A WebSocket one, or one from DialQUICFunc: no QUIC connection gomoqt
	// can reach.
	sess := newSession(&FakeStreamConn{}, NewTrackMux(0), nil, nil, nil, nil, nil, nil, sessionSetup{}, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "test done") }) // not actionable: cleanup

	p, err := sess.AddPath(&quic.Transport{Conn: listenLoopback(t)})

	assert.ErrorIs(t, err, ErrMigrationUnsupported)
	assert.Nil(t, p)
}

// A server session cannot move its client: quic-go refuses a server's path.
func TestSession_AddPath_ServerSession(t *testing.T) {
	sub := subscribeMigrating(t, "moqt")
	tr := &quic.Transport{Conn: listenLoopback(t), ConnectionIDGenerator: zeroConnIDs{}}
	t.Cleanup(func() { _ = tr.Close() }) // not actionable: cleanup

	p, err := sub.serverSession(t).AddPath(tr)

	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrMigrationUnsupported)
	assert.Nil(t, p)
}

// moveTimeout bounds a move in these tests.
const moveTimeout = 5 * time.Second

// zeroConnIDs generates zero-length connection IDs, as a transport of a
// connection that moves should (see Session.AddPath).
type zeroConnIDs struct{}

var _ quic.ConnectionIDGenerator = zeroConnIDs{}

func (zeroConnIDs) GenerateConnectionID() (quic.ConnectionID, error) {
	return quic.ConnectionID{}, nil
}

func (zeroConnIDs) ConnectionIDLen() int { return 0 }

// network is a transport over a loopback socket the test can cut.
type network struct {
	tr   *quic.Transport
	link *fakeLink
}

// newNetwork returns a network whose transport closes after the session,
// since closing a transport the connection used ends the connection.
func newNetwork(tb testing.TB) network {
	tb.Helper()
	link := &fakeLink{PacketConn: listenLoopback(tb)}
	tr := &quic.Transport{Conn: link, ConnectionIDGenerator: zeroConnIDs{}}
	tb.Cleanup(func() { _ = tr.Close() }) // not actionable: cleanup
	return network{tr: tr, link: link}
}

// migratingSub is a session subscribed to a track published every 20 ms,
// dialed through a fakeNAT over dial.
type migratingSub struct {
	sess    *Session
	nat     *fakeNAT
	dial    network
	srv    atomic.Pointer[Session] // the server's side, of the latest session
	groups atomic.Int64             // groups received
}

func subscribeMigrating(tb testing.TB, scheme string) *migratingSub {
	tb.Helper()
	addr := freePort(tb)
	mux := NewTrackMux(0)
	mux.PublishFunc(tb.Context(), migrationTrackPath, publishEvery20ms)
	sub := &migratingSub{}
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(tb)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 10 * time.Second},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			// A dial retried after the server accepted an earlier one leaves
			// that one behind: keep the latest.
			sub.srv.Store(sess)
			<-sess.Context().Done()
		}),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server: %v", err)
		}
	}()
	tb.Cleanup(func() { closeServer(tb, srv) })
	nat := startFakeNAT(tb, addr)

	ctx, cancel := context.WithTimeout(tb.Context(), 10*time.Second)
	defer cancel()
	var (
		sess *Session
		dial network
	)
	require.Eventually(tb, func() bool {
		dial = newNetwork(tb)
		dialer := &Dialer{
			TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only self-signed cert
			// The keep-alive tells the server of a move when nothing else
			// would: a move from a network already gone.
			QUICConfig: &quic.Config{KeepAlivePeriod: time.Second, MaxIdleTimeout: 10 * time.Second},
			Transport:  dial.tr,
		}
		var err error
		sess, err = dialer.Dial(ctx, scheme+"://"+nat.addr()+"/", nil)
		return err == nil
	}, 10*time.Second, 50*time.Millisecond)
	tb.Cleanup(func() { _ = sess.CloseWithError(NoError, "test done") }) // not actionable: cleanup
	tr, err := sess.Subscribe(ctx, migrationTrackPath, "audio", nil)
	require.NoError(tb, err)
	sub.sess, sub.nat, sub.dial = sess, nat, dial
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

// serverSession is the server's side of the session.
func (s *migratingSub) serverSession(tb testing.TB) *Session {
	tb.Helper()
	require.Eventually(tb, func() bool { return s.srv.Load() != nil }, moveTimeout, 5*time.Millisecond,
		"the server should accept the session")
	return s.srv.Load()
}

// server is the QUIC connection on the server's side of the session.
func (s *migratingSub) server(tb testing.TB) *quic.Conn {
	tb.Helper()
	return s.serverSession(tb).conn.(quicConn).QUICConn()
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

func (s *migratingSub) probe(tb testing.TB, p *quic.Path) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), moveTimeout)
	defer cancel()
	require.NoError(tb, p.Probe(ctx))
}

// moveTo adds a path over n, probes it and switches to it.
func (s *migratingSub) moveTo(tb testing.TB, n network) *quic.Path {
	tb.Helper()
	p, err := s.sess.AddPath(n.tr)
	require.NoError(tb, err)
	s.probe(tb, p)
	require.NoError(tb, p.Switch())
	return p
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
	require.Eventually(tb, func() bool { return s.groups.Load()-groups >= 5 }, moveTimeout, 5*time.Millisecond)
}

func listenLoopback(tb testing.TB) net.PacketConn {
	tb.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conn.Close() }) // not actionable: cleanup
	return conn
}
