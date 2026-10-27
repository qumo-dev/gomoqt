package quicgo

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Slot is the socket under a quic.Transport, which it lets change. quic-go
// destroys a connection when any transport it has used is closed
// (quic-go#5975), so a path gives up its socket, not its transport: the
// slot swaps sockets under the transport, which never sees the change.
//
// Neither direction fails the transport for a network going away, which
// would end the connection with it: a write while the slot is empty, or one
// the OS refuses for a network change, is reported as sent, so QUIC treats
// the packet as lost. A socket closed under the slot by anyone but the slot
// leaves it empty until the next Set. Only closing the slot ends its reads.
type Slot struct {
	state atomic.Pointer[slotState]

	mu       sync.Mutex // serializes changes to state
	readBuf  int        // receive buffer size quic-go asked for, applied to each socket
	writeBuf int        // send buffer size likewise
}

// slotState is the slot's socket, nil while empty, and a channel closed when
// the socket changes. A closed slot has closed set.
type slotState struct {
	conn    net.PacketConn
	addr    net.Addr // the last socket's address, kept while empty
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

// Set puts conn in the slot, closing the socket it replaces. A closed slot
// closes conn instead.
func (s *Slot) Set(conn net.PacketConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Load().closed {
		_ = conn.Close() // not actionable: the slot is gone with its connection
		return
	}
	s.applyBuffers(conn)
	s.replace(conn, false)
}

// Clear closes the slot's socket and leaves the slot empty.
func (s *Slot) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Load().closed {
		return
	}
	s.replace(nil, false)
}

// Close closes the slot and its socket, ending its reads.
func (s *Slot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Load().closed {
		return nil
	}
	s.replace(nil, true)
	return nil
}

// replace swaps conn in, closes the socket it replaces and wakes reads
// waiting on the old state. s.mu is held.
func (s *Slot) replace(conn net.PacketConn, closed bool) {
	old := s.state.Load()
	if old.conn != nil {
		_ = old.conn.Close() // not actionable: the socket is being given up
	}
	next := &slotState{conn: conn, addr: old.addr, changed: make(chan struct{}), closed: closed}
	if conn != nil {
		next.addr = conn.LocalAddr()
	}
	s.state.Store(next)
	close(old.changed)
}

// empty empties the slot if conn, closed under it, is still its socket.
func (s *Slot) empty(conn net.PacketConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.state.Load(); !st.closed && st.conn == conn {
		s.replace(nil, false)
	}
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
			s.empty(st.conn)
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

// readBuffered and writeBuffered are sockets whose buffers can be sized, as
// a *net.UDPConn's can.
type (
	readBuffered  interface{ SetReadBuffer(int) error }
	writeBuffered interface{ SetWriteBuffer(int) error }
)

// SetReadBuffer sets the receive buffer of the slot's socket, and of every
// socket put in it later. quic-go raises it through this method.
func (s *Slot) SetReadBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readBuf = bytes
	if c, ok := s.state.Load().conn.(readBuffered); ok {
		return c.SetReadBuffer(bytes)
	}
	return nil
}

// SetWriteBuffer sets the send buffer of the slot's socket, and of every
// socket put in it later.
func (s *Slot) SetWriteBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeBuf = bytes
	if c, ok := s.state.Load().conn.(writeBuffered); ok {
		return c.SetWriteBuffer(bytes)
	}
	return nil
}

// applyBuffers gives conn the buffer sizes asked for so far; the OS caps
// them, as it does for quic-go's own sockets. s.mu is held.
func (s *Slot) applyBuffers(conn net.PacketConn) {
	if c, ok := conn.(readBuffered); ok && s.readBuf > 0 {
		_ = c.SetReadBuffer(s.readBuf) // not actionable: the OS's cap stands
	}
	if c, ok := conn.(writeBuffered); ok && s.writeBuf > 0 {
		_ = c.SetWriteBuffer(s.writeBuf) // not actionable: as above
	}
}

// LocalAddr is the address of the slot's socket, or of the last one while
// the slot is empty.
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
