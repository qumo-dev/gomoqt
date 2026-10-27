package moqt

import (
	"net"
	"sync/atomic"
)

// low-level utility: a network path is added as a socket, net.PacketConn,
// below any gomoqt interface, so the fake sits there.

var _ net.PacketConn = (*fakeLink)(nil)

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
