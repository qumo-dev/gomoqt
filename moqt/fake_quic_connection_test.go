package moqt

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"

	"github.com/okdaichi/gomoqt/transport"
	quicgo "github.com/quic-go/quic-go"
)

var _ StreamConn = (*FakeStreamConn)(nil)

// FakeStreamConn is a fake implementation of StreamConn that models quic-go
// connection semantics:
//   - CloseWithError cancels Context() with *transport.ApplicationError as cause (first-writer-wins, idempotent, always returns nil)
//   - After CloseWithError, AcceptStream/AcceptUniStream/OpenStream/OpenUniStream/OpenStreamSync/OpenUniStreamSync return the close error
//   - Context() returns a context derived from ParentCtx (default: context.Background())
type FakeStreamConn struct {
	mu sync.Mutex

	AcceptStreamFunc      func(ctx context.Context) (transport.Stream, error)
	AcceptUniStreamFunc   func(ctx context.Context) (transport.ReceiveStream, error)
	OpenStreamFunc        func() (transport.Stream, error)
	OpenUniStreamFunc     func() (transport.SendStream, error)
	OpenStreamSyncFunc    func(ctx context.Context) (transport.Stream, error)
	OpenUniStreamSyncFunc func(ctx context.Context) (transport.SendStream, error)
	CloseWithErrorFunc    func(code transport.ConnErrorCode, reason string) error
	ParentCtx             context.Context
	LocalAddrFunc         func() net.Addr
	RemoteAddrFunc        func() net.Addr
	TLSFunc               func() *tls.ConnectionState
	ConnectionStatsFunc   func() quicgo.ConnectionStats

	ctx         context.Context
	cancelCause context.CancelCauseFunc
	closeErr    error // set by first CloseWithError call
}

// ensureContext lazily initialises the internal cancellable context.
// Must be called with m.mu held.
func (m *FakeStreamConn) ensureContext() {
	if m.ctx == nil {
		parent := m.ParentCtx
		if parent == nil {
			parent = context.Background()
		}
		m.ctx, m.cancelCause = context.WithCancelCause(parent)
	}
}

func (m *FakeStreamConn) TLS() *tls.ConnectionState {
	if m.TLSFunc != nil {
		return m.TLSFunc()
	}
	return nil
}

func (m *FakeStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	if m.AcceptStreamFunc != nil {
		return m.AcceptStreamFunc(ctx)
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, io.EOF
}

func (m *FakeStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	if m.AcceptUniStreamFunc != nil {
		return m.AcceptUniStreamFunc(ctx)
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, io.EOF
}

func (m *FakeStreamConn) OpenStream() (transport.Stream, error) {
	if m.OpenStreamFunc != nil {
		return m.OpenStreamFunc()
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, nil
}

func (m *FakeStreamConn) OpenUniStream() (transport.SendStream, error) {
	if m.OpenUniStreamFunc != nil {
		return m.OpenUniStreamFunc()
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, nil
}

func (m *FakeStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	if m.OpenStreamSyncFunc != nil {
		return m.OpenStreamSyncFunc(ctx)
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, nil
}

func (m *FakeStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	if m.OpenUniStreamSyncFunc != nil {
		return m.OpenUniStreamSyncFunc(ctx)
	}
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	return nil, nil
}

func (m *FakeStreamConn) LocalAddr() net.Addr {
	if m.LocalAddrFunc != nil {
		return m.LocalAddrFunc()
	}
	return &net.TCPAddr{}
}

func (m *FakeStreamConn) RemoteAddr() net.Addr {
	if m.RemoteAddrFunc != nil {
		return m.RemoteAddrFunc()
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// CloseWithError models quic-go behavior:
//   - First call stores the close error and cancels Context() with *transport.ApplicationError as cause
//   - Subsequent calls are no-ops
//   - Always returns nil (like quic-go)
//
// If CloseWithErrorFunc is set, it is called instead (for tests that need custom behavior).
func (m *FakeStreamConn) CloseWithError(code transport.ConnErrorCode, reason string) error {
	if m.CloseWithErrorFunc != nil {
		return m.CloseWithErrorFunc(code, reason)
	}
	m.mu.Lock()
	if m.closeErr != nil {
		// Already closed — first-writer-wins, idempotent
		m.mu.Unlock()
		return nil
	}
	m.closeErr = &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(code),
		ErrorMessage: reason,
	}
	m.ensureContext()
	cancel := m.cancelCause
	m.mu.Unlock()
	cancel(m.closeErr)
	return nil
}

func (m *FakeStreamConn) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContext()
	return m.ctx
}

func (m *FakeStreamConn) ConnectionStats() quicgo.ConnectionStats {
	if m.ConnectionStatsFunc != nil {
		return m.ConnectionStatsFunc()
	}
	return quicgo.ConnectionStats{}
}

type FakeWebTransportSession struct {
	FakeStreamConn
	SubprotocolFunc func() string
}

func (m *FakeWebTransportSession) Subprotocol() string {
	if m.SubprotocolFunc != nil {
		return m.SubprotocolFunc()
	}
	return ""
}
