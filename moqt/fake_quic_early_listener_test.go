package moqt

import (
	"context"
	"net"
	"sync"
)

var _ QUICListener = (*FakeEarlyListener)(nil)

// FakeEarlyListener is a fake implementation of QUICListener that models
// quic-go Listener/EarlyListener semantics:
//   - Close() always returns nil, is idempotent (first-writer-wins)
//   - After Close(), Accept() returns the close error (ErrServerClosed by default)
type FakeEarlyListener struct {
	AcceptFunc func(ctx context.Context) (StreamConn, error)
	CloseFunc  func() error
	AddrFunc   func() net.Addr

	mu       sync.Mutex
	closed   bool
	closeErr error         // set by first Close() call
	closeCh  chan struct{} // signalled on Close
}

func (m *FakeEarlyListener) initCloseCh() {
	if m.closeCh == nil {
		m.closeCh = make(chan struct{})
	}
}

func (m *FakeEarlyListener) Accept(ctx context.Context) (StreamConn, error) {
	if m.AcceptFunc != nil {
		return m.AcceptFunc(ctx)
	}
	m.mu.Lock()
	if m.closed {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
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
	if m.AddrFunc != nil {
		return m.AddrFunc()
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// Close models quic-go behavior:
//   - First call stores ErrServerClosed and signals Accept to return
//   - Subsequent calls are no-ops
//   - Always returns nil (like quic-go)
//
// If CloseFunc is set, it is called instead (for tests that need custom behavior).
func (m *FakeEarlyListener) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.closeErr = ErrServerClosed
	m.initCloseCh()
	close(m.closeCh)
	m.mu.Unlock()
	return nil
}
