package moqt

import (
	"fmt"
	"sync"
)

type connManager struct {
	closed bool
	// draining is set once the server shutting down has taken the
	// connections to end: one that comes after would not be among them.
	draining    bool
	mu          sync.Mutex
	connections map[StreamConn]struct{}

	doneChan chan struct{}
}

func newConnManager() *connManager {
	return &connManager{
		connections: make(map[StreamConn]struct{}),
	}
}

func (s *connManager) addConn(conn StreamConn) {
	if conn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.draining {
		return
	}
	if len(s.connections) == 0 {
		s.doneChan = make(chan struct{})
	}
	s.connections[conn] = struct{}{}
}

// tracks reports whether conn is among the manager's connections. A
// connection that came once the manager was draining is not: the shutdown
// that drained it neither ends that connection nor waits for it.
func (s *connManager) tracks(conn StreamConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.connections[conn]
	return ok
}

// drain returns the connections for a shutdown to end, and takes no more:
// a connection is either in what it returns or refused by addConn, so none
// is left that the shutdown does not know of.
func (s *connManager) drain() []StreamConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draining = true
	conns := make([]StreamConn, 0, len(s.connections))
	for c := range s.connections {
		conns = append(conns, c)
	}
	return conns
}

func (s *connManager) removeConn(conn StreamConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	delete(s.connections, conn)

	if len(s.connections) == 0 {
		if s.doneChan != nil {
			close(s.doneChan)
			s.doneChan = nil
		}
	}
}

func (s *connManager) countSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.connections)
}

// conns returns a snapshot of the current connections. The returned slice may
// be iterated while connections are concurrently added or removed (e.g. during
// Server.Close/Shutdown, where closing a connection triggers removeConn on the
// live map); iterating the map directly in those paths is a data race.
func (s *connManager) conns() []StreamConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns := make([]StreamConn, 0, len(s.connections))
	for c := range s.connections {
		conns = append(conns, c)
	}
	return conns
}

func (s *connManager) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doneChan == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.doneChan
}

func (s *connManager) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if len(s.connections) != 0 {
		return fmt.Errorf("cannot close session manager with active sessions")
	}
	s.closed = true
	return nil
}
