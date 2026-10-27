package moqt

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/quicgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Path tests run on a bare QUIC connection whose server only sends, so that
// what moves is the connection itself, not a session's streams. The client
// reaches the server through a fakeNAT, which can move the default path's
// socket to a new public address, as the OS does when it sends that socket,
// bound to no network, over cellular once Wi-Fi is gone.

// A receive-only client moves back and forth between two networks, as a
// phone between Wi-Fi and cellular, and the server keeps sending to it over
// whichever it is on, on the same connection.
func TestPath_Switch_RepeatedMoves(t *testing.T) {
	tests := map[string]struct {
		cutBeforeMove bool
	}{
		// The application moves while the old network still works.
		"old network still up": {},
		// The old network is gone before the move.
		"old network already gone": {cutBeforeMove: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.cutBeforeMove {
				skipDeadNetworkMove(t)
			}
			c := dialPushed(t)
			wifi, wifiLink := c.addLink(t)
			cell, cellLink := c.addLink(t)
			c.moveTo(t, wifi)
			c.requireDataOn(t, wifiLink)
			cur, curLink, next, nextLink := wifi, wifiLink, cell, cellLink

			for move := 1; move <= 8; move++ {
				nextLink.down.Store(false) // the network moved to is up
				if tt.cutBeforeMove {
					curLink.down.Store(true)
				}

				c.moveTo(t, next)
				// Make before break: the old network goes only once the new
				// path carries data. Cut sooner, the client's answer to the
				// server's PATH_CHALLENGE can go out on the old path and be
				// lost (quic-go#5408), which a v0.63 server never asks again.
				c.requireDataOn(t, nextLink, "move "+strconv.Itoa(move))
				curLink.down.Store(true)

				c.requireDataOn(t, nextLink, "move "+strconv.Itoa(move))
				cur, curLink, next, nextLink = next, nextLink, cur, curLink
			}
			assert.NoError(t, c.conn.Context().Err(), "the connection should survive every move")
		})
	}
}

// The default path, probed while the connection was on another network,
// still takes the connection after the OS has moved its socket to another
// network, so that the server sees it at an address it never validated:
// the server follows the connection to that address and keeps sending.
//
// quic-go's Switch does not validate again, so this shows end to end that
// the server, not the client's earlier probe, validates the new address.
func TestDefaultPath_AfterRouteChange(t *testing.T) {
	tests := map[string]struct {
		probeBeforeChange bool
	}{
		// Kept ready: probed while Wi-Fi was up, switched to without a probe.
		"probed before the route change": {probeBeforeChange: true},
		// Not kept ready: probed after the route change, then switched to.
		"probed after the route change": {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := dialPushed(t)
			wifi, wifiLink := c.addLink(t)
			c.moveTo(t, wifi)
			c.requireDataOn(t, wifiLink)
			def := c.set.def
			if tt.probeBeforeChange {
				c.probe(t, def)
			}

			// Wi-Fi is gone: the bound path's network with it, and the OS
			// sends the default path's socket over cellular.
			wifiLink.down.Store(true)
			newAddr := c.nat.remap(t, c.defaultSocketAddr())
			start := time.Now()
			if !tt.probeBeforeChange {
				c.probe(t, def)
			}
			require.NoError(t, def.Switch())

			before := c.received.Load()
			require.Eventually(t, func() bool { return c.received.Load() > before+4000 }, moveTimeout, 5*time.Millisecond,
				"data should resume over the default path")
			t.Logf("data resumed %s after the switch", time.Since(start).Round(time.Millisecond))
			assert.Eventually(t, func() bool { return c.server().RemoteAddr().String() == newAddr.String() },
				moveTimeout, 10*time.Millisecond, "the server should send to the default path's new address")
		})
	}
}

// On the default path, the connection follows the OS's default network by
// itself: when the OS moves the socket to another network, the server
// follows, with nothing for the application to do.
func TestDefaultPath_PassiveFallback(t *testing.T) {
	c := dialPushed(t)

	newAddr := c.nat.remap(t, c.defaultSocketAddr())

	before := c.received.Load()
	require.Eventually(t, func() bool { return c.received.Load() > before+4000 }, moveTimeout, 5*time.Millisecond)
	assert.Eventually(t, func() bool { return c.server().RemoteAddr().String() == newAddr.String() },
		moveTimeout, 10*time.Millisecond)
}

// The default path lives as long as the session, active or not.
func TestDefaultPath_Close(t *testing.T) {
	c := dialPushed(t)
	def := c.set.def
	assert.ErrorIs(t, def.Close(), ErrDefaultPath, "while active")

	p, link := c.addLink(t)
	c.moveTo(t, p)
	c.requireDataOn(t, link)

	assert.ErrorIs(t, def.Close(), ErrDefaultPath, "while not active")
}

// The path the connection is on cannot be closed: the application switches
// away from it first.
func TestPath_Close_Active(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)
	c.moveTo(t, p)

	err := p.Close()

	assert.ErrorIs(t, err, ErrPathActive)
	assert.False(t, link.closed.Load())
	c.requireDataOn(t, link)
}

// A path the connection has left closes, and gives up its socket, at once.
func TestPath_Close_Left(t *testing.T) {
	c := dialPushed(t)
	old, oldLink := c.addLink(t)
	next, nextLink := c.addLink(t)
	c.moveTo(t, old)
	c.moveTo(t, next)
	// Make before break, as in TestPath_Switch_RepeatedMoves: closed sooner,
	// the old socket can take the client's answer to the server's
	// PATH_CHALLENGE with it.
	c.requireDataOn(t, nextLink)

	require.NoError(t, old.Close())

	assert.True(t, oldLink.closed.Load())
	c.requireDataOn(t, nextLink)
}

// A path never used gives up its socket as soon as it is closed.
func TestPath_Close_Unused(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)

	require.NoError(t, p.Close())

	assert.True(t, link.closed.Load())
}

func TestPath_Closed(t *testing.T) {
	c := dialPushed(t)
	p, _ := c.addLink(t)
	require.NoError(t, p.Close())
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	assert.ErrorIs(t, p.Probe(ctx), ErrPathClosed)
	assert.ErrorIs(t, p.Switch(), ErrPathClosed)
	assert.NoError(t, p.Close(), "closing again is a no-op")
}

// Probing a path already validated returns at once, and the path stays
// ready to switch to.
func TestPath_Probe_AlreadyValidated(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)
	c.probe(t, p)

	start := time.Now()
	c.probe(t, p)

	assert.Less(t, time.Since(start), 100*time.Millisecond)
	require.NoError(t, p.Switch())
	c.requireDataOn(t, link)
}

// A probe ends with the connection, even with no deadline of its own.
func TestPath_Probe_EndsWithConnection(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)
	link.down.Store(true) // the probe gets no answer
	got := make(chan error, 1)
	go func() { got <- p.Probe(context.Background()) }()

	require.NoError(t, c.conn.CloseWithError(0, "done"))

	select {
	case err := <-got:
		assert.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("the probe did not end with the connection")
	}
}

// Moves made back to back, each probe right after the switch before it,
// keep finding a connection ID to probe with: Switch closes the path left,
// and the server replaces its connection ID.
func TestPath_Switch_BackToBack(t *testing.T) {
	c := dialPushed(t)
	a, _ := c.addLink(t)
	b, bLink := c.addLink(t)

	for move := range 12 {
		p := a
		if move%2 == 1 {
			p = b
		}
		c.moveTo(t, p)
	}

	c.requireDataOn(t, bLink)
	assert.NoError(t, c.conn.Context().Err())
}

func TestPathSet_Add_ConnectionGone(t *testing.T) {
	c := dialPushed(t)
	require.NoError(t, c.conn.CloseWithError(0, "done"))
	link := &fakeLink{PacketConn: listenLoopback(t)}

	p, err := c.set.add(link)

	assert.Error(t, err)
	assert.Nil(t, p)
	assert.True(t, link.closed.Load(), "the socket should be closed when no path takes it")
}

func TestPathSet_ClosesPathSocketsWithConnection(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)
	c.moveTo(t, p)

	require.NoError(t, c.conn.CloseWithError(0, "done"))

	assert.Eventually(t, link.closed.Load, 3*time.Second, 10*time.Millisecond)
}

// pushedClient is a client that only receives: the server sends it a steady
// stream, and it sends nothing but acknowledgements and keep-alives.
type pushedClient struct {
	conn     *quic.Conn
	tr       *quic.Transport
	set      *pathSet
	nat      *fakeNAT
	servers  chan *quic.Conn
	srv      atomic.Pointer[quic.Conn]
	received atomic.Int64 // bytes read from the server's stream
}

func dialPushed(tb testing.TB) *pushedClient {
	tb.Helper()
	addr, servers := startPushServer(tb)
	nat := startFakeNAT(tb, addr)
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	conn, tr, err := quicgo.Dial(ctx, nat.addr(), &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test-only self-signed cert
		NextProtos:         []string{pushALPN},
		MinVersion:         tls.VersionTLS13,
	}, &quic.Config{
		// The keep-alive tells the server of a move when nothing else would:
		// a move from a network already gone.
		KeepAlivePeriod: time.Second,
		MaxIdleTimeout:  10 * time.Second,
	})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conn.CloseWithError(0, "test done") }) // not actionable: cleanup
	str, err := conn.AcceptUniStream(ctx)
	require.NoError(tb, err)
	c := &pushedClient{conn: conn, tr: tr, set: newPathSet(conn, tr), nat: nat, servers: servers}
	// The read ends with the connection, closed by the cleanup above.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := str.Read(buf)
			c.received.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	return c
}

// server is the server's side of the connection.
func (c *pushedClient) server() *quic.Conn {
	if s := c.srv.Load(); s != nil {
		return s
	}
	s := <-c.servers
	c.srv.Store(s)
	return s
}

// defaultSocketAddr is the address the default path's socket sends from,
// as the fakeNAT sees it.
func (c *pushedClient) defaultSocketAddr() net.Addr {
	port := c.tr.Conn.LocalAddr().(*net.UDPAddr).Port
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

// addLink adds a path over a new loopback socket the test can cut.
func (c *pushedClient) addLink(tb testing.TB) (*Path, *fakeLink) {
	tb.Helper()
	link := &fakeLink{PacketConn: listenLoopback(tb)}
	p, err := c.set.add(link)
	require.NoError(tb, err)
	return p, link
}

func (c *pushedClient) probe(tb testing.TB, p *Path) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), moveTimeout)
	defer cancel()
	require.NoError(tb, p.Probe(ctx))
}

func (c *pushedClient) moveTo(tb testing.TB, p *Path) {
	tb.Helper()
	c.probe(tb, p)
	require.NoError(tb, p.Switch())
}

// requireDataOn waits for the server's stream to arrive over link: a dozen
// datagrams, more than a stray retransmission or probe.
func (c *pushedClient) requireDataOn(tb testing.TB, link *fakeLink, msgAndArgs ...any) {
	tb.Helper()
	before := link.reads.Load()
	require.Eventually(tb, func() bool { return link.reads.Load()-before >= 12 }, moveTimeout, 5*time.Millisecond, msgAndArgs...)
}

const pushALPN = "moqt-path-test"

// startPushServer starts a QUIC server that, on each connection, sends 200
// bytes every 10 ms on a unidirectional stream. It returns its address and
// the connections it accepts.
func startPushServer(tb testing.TB) (string, chan *quic.Conn) {
	tb.Helper()
	ln, err := quic.ListenAddrEarly("127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{generateTestCert(tb)},
		NextProtos:   []string{pushALPN},
		MinVersion:   tls.VersionTLS13,
	}, &quic.Config{MaxIdleTimeout: 10 * time.Second})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = ln.Close() }) // not actionable: cleanup
	conns := make(chan *quic.Conn, 1)
	// The server's goroutines end with the test: its context is cancelled
	// before cleanups run, and the listener and connections close with it.
	ctx := tb.Context()
	go func() {
		for {
			conn, err := ln.Accept(ctx)
			if err != nil {
				return // the test is over or the listener is closed
			}
			select {
			case conns <- conn:
			default:
			}
			go push(ctx, conn)
		}
	}()
	return ln.Addr().(*net.UDPAddr).String(), conns
}

func push(ctx context.Context, conn *quic.Conn) {
	str, err := conn.OpenUniStream()
	if err != nil {
		return // the connection is gone
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	buf := make([]byte, 200)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := str.Write(buf); err != nil {
			return // the connection is gone
		}
	}
}
