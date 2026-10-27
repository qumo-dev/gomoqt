package quicgo

import (
	"context"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			assert.NoError(t, c.m.conn.Context().Err(), "the connection should survive every move")
		})
	}
}

// Closing the path the connection is on moves it onto a new socket bound to
// no network, which follows the OS's default network, and gives up the
// closed path's socket at once.
func TestPath_Close_Active(t *testing.T) {
	tests := map[string]struct {
		cutBeforeClose bool
	}{
		"closed path still up":       {},
		"closed path's network gone": {cutBeforeClose: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := dialPushed(t)
			p, link := c.addLink(t)
			c.moveTo(t, p)
			c.requireDataOn(t, link)
			if tt.cutBeforeClose {
				link.down.Store(true)
			}

			require.NoError(t, p.Close())

			assert.True(t, link.closed.Load(), "the closed path's socket should be given up at once")
			before := c.received.Load()
			assert.Eventually(t, func() bool { return c.received.Load() > before+4000 }, 5*time.Second, 10*time.Millisecond,
				"data should keep arriving over the new socket")
		})
	}
}

// A path the connection has left, then closed, gives up its socket once it
// is retired, on the next call, and the connection stays on the new path.
func TestPath_Close_Left(t *testing.T) {
	c := dialPushed(t)
	old, oldLink := c.addLink(t)
	next, nextLink := c.addLink(t)
	c.moveTo(t, old)
	c.moveTo(t, next)
	c.requireDataOn(t, nextLink)

	require.NoError(t, old.Close())
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, next.Probe(ctx)) // any call retires the paths left

	assert.True(t, oldLink.closed.Load(), "the closed path's socket should be given up once retired")
	c.requireDataOn(t, nextLink)
}

// A path never used gives up its socket as soon as it is closed.
func TestPath_Close_Unused(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)

	require.NoError(t, p.Close())

	assert.True(t, link.closed.Load())
}

func TestPath_Switch_NotValidated(t *testing.T) {
	c := dialPushed(t)
	p, _ := c.addLink(t)

	err := p.Switch()

	assert.ErrorIs(t, err, ErrPathNotValidated)
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

// Moves made back to back, each probe right after the switch before it,
// keep finding a connection ID to probe with: the path left is retired as
// the next probe starts, and the server replaces its connection ID.
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
	assert.NoError(t, c.m.conn.Context().Err())
}

func TestMigrator_AddPath_ConnectionGone(t *testing.T) {
	c := dialPushed(t)
	require.NoError(t, c.m.conn.CloseWithError(0, "done"))
	conn := &fakePacketConn{}

	p, err := c.m.AddPath(conn)

	assert.Error(t, err)
	assert.Nil(t, p)
	assert.True(t, conn.isClosed(), "the socket should be closed when no path takes it")
}

func TestMigrator_ClosesPathSocketsWithConnection(t *testing.T) {
	c := dialPushed(t)
	p, link := c.addLink(t)
	c.moveTo(t, p)

	require.NoError(t, c.m.conn.CloseWithError(0, "done"))

	assert.Eventually(t, link.closed.Load, 3*time.Second, 10*time.Millisecond)
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

// pushedClient is a client that only receives: the server sends it a steady
// stream, and it sends nothing but acknowledgements and keep-alives.
type pushedClient struct {
	m        *Migrator
	received atomic.Int64 // bytes read from the server's stream
}

func dialPushed(tb testing.TB) *pushedClient {
	tb.Helper()
	srv := startPushServer(tb)
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	m, err := Dial(ctx, srv.addr(), srv.clientTLS("localhost"), &quic.Config{
		// The keep-alive tells the server of a move when nothing else would:
		// the first move off the home path, from a network already gone.
		KeepAlivePeriod: time.Second,
		MaxIdleTimeout:  10 * time.Second,
	})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = m.conn.CloseWithError(0, "test done") }) // not actionable: cleanup
	str, err := m.conn.AcceptUniStream(ctx)
	require.NoError(tb, err)
	c := &pushedClient{m: m}
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

// addLink adds a path over a new loopback socket the test can cut.
func (c *pushedClient) addLink(tb testing.TB) (*Path, *fakeLink) {
	tb.Helper()
	link := &fakeLink{PacketConn: listenLoopback(tb)}
	p, err := c.m.AddPath(link)
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

// startPushServer starts a QUIC server that, on each connection, sends a
// steady stream of 200 bytes every 10 ms on a unidirectional stream.
func startPushServer(tb testing.TB) *echoServer {
	tb.Helper()
	cert, leaf := localhostCert(tb)
	ln, err := quic.ListenAddrEarly("127.0.0.1:0", serverTLS(cert), &quic.Config{MaxIdleTimeout: 10 * time.Second})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = ln.Close() }) // not actionable: cleanup
	// The server's goroutines end with the test: its context is cancelled
	// before cleanups run, and the listener and connections close with it.
	ctx := tb.Context()
	go func() {
		for {
			conn, err := ln.Accept(ctx)
			if err != nil {
				return // the test is over or the listener is closed
			}
			go push(ctx, conn)
		}
	}()
	return &echoServer{port: ln.Addr().(*net.UDPAddr).Port, cert: leaf}
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
