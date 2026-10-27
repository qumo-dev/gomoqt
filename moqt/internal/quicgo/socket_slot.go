package quicgo

import (
	"errors"
	"net"
	"sync"
	"time"
)

// socketSlot is the socket under one network path's quic.Transport. quic-go
// destroys a connection when any transport it has used is closed, so a path
// the application is done with keeps its transport for the next path and
// gives up only its socket: the slot closes the socket and waits, empty, for
// the next one.
//
// Neither direction fails the transport for a network going away, which
// would end the connection with it: a write while empty, or one refused for
// a network change, is reported as sent, and a failed read is retried, or,
// if the socket was closed under the slot, waits for the next socket. Only
// closing the slot ends its reads.
type socketSlot struct {
	mu      sync.Mutex
	conn    net.PacketConn // nil while empty
	addr    net.Addr       // the last socket's address, kept while empty
	closed  bool
	changed chan struct{} // closed, and replaced, whenever conn changes
}

var _ net.PacketConn = (*socketSlot)(nil)

func newSocketSlot(conn net.PacketConn) *socketSlot {
	return &socketSlot{conn: conn, addr: conn.LocalAddr(), changed: make(chan struct{})}
}

// set puts conn in the empty slot. A closed slot closes conn instead.
func (s *socketSlot) set(conn net.PacketConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close() // not actionable: the slot is gone with its connection
		return
	}
	s.replace(conn)
}

// clear closes the slot's socket, leaving it empty.
func (s *socketSlot) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replace(nil)
}

// replace swaps conn in and closes the socket it replaces. s.mu is held.
func (s *socketSlot) replace(conn net.PacketConn) {
	if old := s.conn; old != nil {
		_ = old.Close() // not actionable: the socket is being given up
	}
	s.conn = conn
	if conn != nil {
		s.addr = conn.LocalAddr()
	}
	close(s.changed)
	s.changed = make(chan struct{})
}

// current is the slot's socket, nil while empty, and a channel closed when
// that changes. A closed slot reports net.ErrClosed.
func (s *socketSlot) current() (net.PacketConn, <-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, net.ErrClosed
	}
	return s.conn, s.changed, nil
}

func (s *socketSlot) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		conn, changed, err := s.current()
		if err != nil {
			return 0, nil, err
		}
		if conn == nil {
			<-changed
			continue
		}
		n, addr, err := conn.ReadFrom(p)
		if err == nil {
			return n, addr, nil
		}
		if !errors.Is(err, net.ErrClosed) {
			// A read error the socket survives, such as Windows reporting an
			// ICMP port unreachable: read again, unhurried.
			time.Sleep(readRetryDelay)
			continue
		}
		s.mu.Lock()
		if s.conn == conn && !s.closed {
			// The socket was closed under the slot: wait for the next.
			s.replace(nil)
		}
		s.mu.Unlock()
	}
}

// readRetryDelay spaces the reads of a socket that keeps failing.
const readRetryDelay = 10 * time.Millisecond

func (s *socketSlot) WriteTo(p []byte, addr net.Addr) (int, error) {
	conn, _, err := s.current()
	if err != nil {
		return 0, err
	}
	if conn == nil {
		return len(p), nil // no socket: lost, as on a network that is gone
	}
	n, err := conn.WriteTo(p, addr)
	if err != nil && (isNetChangeErr(err) || errors.Is(err, net.ErrClosed)) {
		return len(p), nil // lost: QUIC retransmits on whichever path is active
	}
	return n, err
}

// Close closes the slot and its socket, ending its reads.
func (s *socketSlot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.replace(nil)
	s.closed = true
	return nil
}

func (s *socketSlot) LocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *socketSlot) SetDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetDeadline(t) })
}

func (s *socketSlot) SetReadDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetReadDeadline(t) })
}

func (s *socketSlot) SetWriteDeadline(t time.Time) error {
	return s.forward(func(c net.PacketConn) error { return c.SetWriteDeadline(t) })
}

// forward applies f to the slot's socket, if it has one.
func (s *socketSlot) forward(f func(net.PacketConn) error) error {
	conn, _, err := s.current()
	if err != nil || conn == nil {
		return err
	}
	return f(conn)
}
