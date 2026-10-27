package quicgo

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// low-level utility: a network path is a socket, below any gomoqt interface,
// so the fakes sit at net.PacketConn, the type a path is added with.

var (
	_ net.PacketConn = (*fakeLink)(nil)
	_ net.PacketConn = (*fakePacketConn)(nil)
)

// fakeLink is a real UDP socket whose network can be cut: while down, it
// drops every datagram it sends or receives, as a lost network does, and
// writes report success. It counts the datagrams it reads, to show which
// path the server sends over, and records whether it was closed.
type fakeLink struct {
	net.PacketConn
	down   atomic.Bool
	reads  atomic.Int64
	closed atomic.Bool
}

func (c *fakeLink) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil {
			return n, addr, err
		}
		if !c.down.Load() {
			c.reads.Add(1)
			return n, addr, nil
		}
	}
}

func (c *fakeLink) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.down.Load() {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *fakeLink) Close() error {
	c.closed.Store(true)
	return c.PacketConn.Close()
}

// fakePacketConn is a socket with no network: writes return writeErrs in
// order, the last repeating, or succeed once it is empty; reads block until
// it is closed. Close is idempotent.
type fakePacketConn struct {
	writeErrs []error

	mu     sync.Mutex
	writes int
	once   sync.Once
	done   chan struct{}
}

func (c *fakePacketConn) closedCh() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done == nil {
		c.done = make(chan struct{})
	}
	return c.done
}

func (c *fakePacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closedCh()
	return 0, nil, net.ErrClosed
}

func (c *fakePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writeErrs) == 0 {
		return len(p), nil
	}
	i := min(c.writes, len(c.writeErrs)-1)
	c.writes++
	if err := c.writeErrs[i]; err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *fakePacketConn) Close() error {
	done := c.closedCh()
	c.once.Do(func() { close(done) })
	return nil
}

func (c *fakePacketConn) isClosed() bool {
	select {
	case <-c.closedCh():
		return true
	default:
		return false
	}
}

func (c *fakePacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
}
func (c *fakePacketConn) SetDeadline(time.Time) error      { return nil }
func (c *fakePacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakePacketConn) SetWriteDeadline(time.Time) error { return nil }
