package quicgo

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Slot is the socket under a quic.Transport. quic-go destroys a connection
// when any transport it has used is closed, or fails to read (quic-go#5975),
// so a path that is done gives up its socket, not its transport: Clear
// closes the socket and leaves the slot empty, and the transport, idle, is
// closed with the connection.
//
// Neither direction fails the transport for a network going away, which
// would end the connection with it: a write while the slot is empty, or one
// the OS refuses for a network change, is reported as sent, so QUIC treats
// the packet as lost, and a socket closed under the slot leaves it empty.
// Only closing the slot ends its reads.
type Slot struct {
	state atomic.Pointer[slotState]
	mu    sync.Mutex // serializes changes to state
}

// slotState is the slot's socket, nil once empty, and a channel closed when
// the state changes.
type slotState struct {
	conn    net.PacketConn
	addr    net.Addr // the socket's address, kept once empty
	changed chan struct{}
	closed  bool
}

var _ net.PacketConn = (*Slot)(nil)

// newSlot returns a slot holding conn.
func newSlot(conn net.PacketConn) *Slot {
	s := &Slot{}
	s.state.Store(&slotState{conn: conn, addr: conn.LocalAddr(), changed: make(chan struct{})})
	return s
}

// Clear closes the slot's socket and leaves the slot empty.
func (s *Slot) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.state.Load(); !st.closed && st.conn != nil {
		s.next(false)
	}
}

// Close closes the slot and its socket, ending its reads.
func (s *Slot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.Load().closed {
		s.next(true)
	}
	return nil
}

// next closes the socket, stores the empty state, closed or not, and wakes
// reads waiting on the old state. s.mu is held.
func (s *Slot) next(closed bool) {
	old := s.state.Load()
	if old.conn != nil {
		_ = old.conn.Close() // not actionable: the socket is being given up
	}
	s.state.Store(&slotState{addr: old.addr, changed: make(chan struct{}), closed: closed})
	close(old.changed)
}

// ReadFrom reads a datagram from the slot's socket, waiting while the slot
// is empty. It returns net.ErrClosed once the slot is closed, and a deadline
// set through SetReadDeadline as the socket returns it, which is how
// quic.Transport.Close stops its reads.
func (s *Slot) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		st := s.state.Load()
		if st.closed {
			return 0, nil, net.ErrClosed
		}
		if st.conn == nil {
			<-st.changed
			continue
		}
		n, addr, err := st.conn.ReadFrom(p)
		if err == nil {
			return n, addr, nil
		}
		if errors.Is(err, net.ErrClosed) {
			s.Clear() // closed under the slot: empty it
			continue
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return 0, nil, err
		}
		// A read error the socket survives, such as Windows reporting an
		// ICMP port unreachable: read again, unhurried.
		time.Sleep(readRetryDelay)
	}
}

// readRetryDelay spaces the reads of a socket that keeps failing.
const readRetryDelay = 10 * time.Millisecond

// WriteTo writes a datagram from the slot's socket. A write while the slot
// is empty, or one refused for a network change, is reported as sent.
func (s *Slot) WriteTo(p []byte, addr net.Addr) (int, error) {
	st := s.state.Load()
	if st.closed {
		return 0, net.ErrClosed
	}
	if st.conn == nil {
		return len(p), nil // no socket: lost, as on a network that is gone
	}
	n, err := st.conn.WriteTo(p, addr)
	if err != nil && (isNetChangeErr(err) || errors.Is(err, net.ErrClosed)) {
		return len(p), nil // lost: QUIC retransmits on whichever path is active
	}
	return n, err
}

// SetReadBuffer sets the receive buffer of the slot's socket, if it has
// one that can be sized. quic-go raises it through this method.
func (s *Slot) SetReadBuffer(bytes int) error {
	if c, ok := s.state.Load().conn.(interface{ SetReadBuffer(int) error }); ok {
		return c.SetReadBuffer(bytes)
	}
	return nil
}

// SetWriteBuffer sets the send buffer of the slot's socket likewise.
func (s *Slot) SetWriteBuffer(bytes int) error {
	if c, ok := s.state.Load().conn.(interface{ SetWriteBuffer(int) error }); ok {
		return c.SetWriteBuffer(bytes)
	}
	return nil
}

// LocalAddr is the address of the slot's socket, kept once the slot is
// empty.
func (s *Slot) LocalAddr() net.Addr { return s.state.Load().addr }

// SetDeadline sets the deadlines of the slot's socket.
func (s *Slot) SetDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetDeadline(t) })
}

// SetReadDeadline sets the read deadline of the slot's socket.
func (s *Slot) SetReadDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetReadDeadline(t) })
}

// SetWriteDeadline sets the write deadline of the slot's socket.
func (s *Slot) SetWriteDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetWriteDeadline(t) })
}

// forward applies f to the slot's socket, if it has one.
func (s *Slot) forward(f func(net.PacketConn) error) error {
	st := s.state.Load()
	if st.closed {
		return net.ErrClosed
	}
	if st.conn == nil {
		return nil
	}
	return f(st.conn)
}
