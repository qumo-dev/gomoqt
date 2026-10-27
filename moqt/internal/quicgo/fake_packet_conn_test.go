package quicgo

import (
	"net"
	"sync"
	"time"
)

// low-level utility: a slot holds a socket, below any gomoqt interface, so
// the fake sits at net.PacketConn.

var _ net.PacketConn = (*fakePacketConn)(nil)

// fakePacketConn is a socket with no network: writes return writeErrs in
// order, the last repeating, or succeed once it is empty; reads block until
// it is closed; buffer sizes set are recorded. Close is idempotent.
type fakePacketConn struct {
	writeErrs []error

	mu       sync.Mutex
	writes   int
	readBuf  int // last receive buffer size set
	writeBuf int // last send buffer size set
	once     sync.Once
	done     chan struct{}
}

func (c *fakePacketConn) SetReadBuffer(bytes int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readBuf = bytes
	return nil
}

func (c *fakePacketConn) SetWriteBuffer(bytes int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeBuf = bytes
	return nil
}

// buffers are the buffer sizes last set.
func (c *fakePacketConn) buffers() (read, write int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readBuf, c.writeBuf
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
