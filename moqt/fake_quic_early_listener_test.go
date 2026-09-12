package moqt

import (
	"context"
	"net"
	"sync"
)

var _ QUICListener = (*FakeEarlyListener)(nil)

// connResult is one queued outcome for a listener accept.
type connResult struct {
	Conn StreamConn
	Err  error
}

// FakeEarlyListener is a fake implementation of QUICListener that models
// quic-go Listener/EarlyListener semantics:
//   - Close() always returns nil, is idempotent (first-writer-wins)
//   - After Close(), Accept() returns the close error (ErrServerClosed by default)
//
// Accepts is a results queue: entries are returned in order and the last entry
// repeats once exhausted. An empty queue blocks until Close or ctx cancellation,
// which is the usual shape for a listener under test.
type FakeEarlyListener struct {
	Accepts []connResult

	// AddrValue overrides the reported listen address.
	AddrValue net.Addr

	mu        sync.Mutex
	acceptIdx int
	closed    bool
	closeErr  error         // set by first Close() call
	closeCh   chan struct{} // signalled on Close
}

func (m *FakeEarlyListener) initCloseCh() {
	if m.closeCh == nil {
		m.closeCh = make(chan struct{})
	}
}

func (m *FakeEarlyListener) Accept(ctx context.Context) (StreamConn, error) {
	m.mu.Lock()
	if m.closed {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.Accepts) > 0 {
		r := m.Accepts[min(m.acceptIdx, len(m.Accepts)-1)]
		m.acceptIdx++
		m.mu.Unlock()
		return r.Conn, r.Err
	}
	m.initCloseCh()
	ch := m.closeCh
	m.mu.Unlock()

	// Block until Close() or ctx cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ch:
		m.mu.Lock()
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
}

func (m *FakeEarlyListener) Addr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AddrValue != nil {
		return m.AddrValue
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// Close models quic-go behavior:
//   - First call stores ErrServerClosed and signals Accept to return
//   - Subsequent calls are no-ops
//   - Always returns nil (like quic-go)
func (m *FakeEarlyListener) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	m.closeErr = ErrServerClosed
	m.initCloseCh()
	close(m.closeCh)
	return nil
}
