package moqt

import (
	"fmt"
	"sync"
)

type sessionManager struct {
	closed   bool
	mu       sync.Mutex
	sessions map[*Session]struct{}

	doneChan chan struct{}
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		sessions: make(map[*Session]struct{}),
	}
}

func (s *sessionManager) addSession(sess *Session) {
	if sess == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(s.sessions) == 0 {
		s.doneChan = make(chan struct{})
	}
	s.sessions[sess] = struct{}{}
}

func (s *sessionManager) removeSession(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	delete(s.sessions, sess)

	if len(s.sessions) == 0 {
		if s.doneChan != nil {
			close(s.doneChan)
			s.doneChan = nil
		}
	}
}

func (s *sessionManager) countSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *sessionManager) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doneChan == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.doneChan
}

func (s *sessionManager) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if len(s.sessions) != 0 {
		return fmt.Errorf("cannot close session manager with active sessions")
	}
	s.closed = true
	return nil
}
