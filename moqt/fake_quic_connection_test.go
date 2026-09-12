package moqt

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"

	quicgo "github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

var _ StreamConn = (*FakeStreamConn)(nil)

// biStreamResult is one queued outcome for a bidirectional stream open/accept.
// Block parks the call until the connection context is done, modelling a peer
// that opens nothing further without hanging up.
type biStreamResult struct {
	Stream transport.Stream
	Err    error
	Block  bool
}

// sendStreamResult is one queued outcome for a unidirectional stream open.
type sendStreamResult struct {
	Stream transport.SendStream
	Err    error
	Block  bool
}

// recvStreamResult is one queued outcome for a unidirectional stream accept.
type recvStreamResult struct {
	Stream transport.ReceiveStream
	Err    error
	Block  bool
}

// closeCall records one CloseWithError invocation.
type closeCall struct {
	Code   transport.ConnErrorCode
	Reason string
}

// FakeStreamConn is a fake implementation of StreamConn that models quic-go
// connection semantics:
//   - CloseWithError cancels Context() with *transport.ApplicationError as cause (first-writer-wins, idempotent, always returns nil)
//   - After CloseWithError, AcceptStream/AcceptUniStream/OpenStream/OpenUniStream/OpenStreamSync/OpenUniStreamSync return the close error
//   - Context() returns a context derived from ParentCtx (default: context.Background())
//
// Stream open/accept behavior is driven by results queues: entries are returned
// in order and the last entry repeats once exhausted. An empty queue returns
// io.EOF for accepts and a nil stream for opens.
type FakeStreamConn struct {
	mu sync.Mutex

	AcceptStreams    []biStreamResult
	AcceptUniStreams []recvStreamResult
	OpenStreams      []biStreamResult
	OpenUniStreams   []sendStreamResult

	ParentCtx context.Context

	// Value overrides; the zero value yields the documented default.
	LocalAddrValue  net.Addr
	RemoteAddrValue net.Addr
	TLSState        *tls.ConnectionState
	Stats           quicgo.ConnectionStats
	CloseErr        error // returned by CloseWithError instead of nil

	// StatsBytesSentStep advances the reported BytesSent by this much on every
	// ConnectionStats call, modelling a connection with ongoing traffic.
	StatsBytesSentStep uint64

	// Notification channels; sends are non-blocking.
	CloseNotify  chan<- closeCall
	AcceptNotify chan<- struct{}

	acceptIdx    int
	acceptUniIdx int
	openIdx      int
	openUniIdx   int

	closeCalls []closeCall

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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TLSState
}

// waitDone parks until the connection context is done, then reports the reason.
func (m *FakeStreamConn) waitDone() error {
	m.mu.Lock()
	m.ensureContext()
	ctx := m.ctx
	m.mu.Unlock()

	<-ctx.Done()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeErr != nil {
		return m.closeErr
	}
	return context.Cause(ctx)
}

func (m *FakeStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	m.mu.Lock()
	signal(m.AcceptNotify)
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.AcceptStreams) == 0 {
		m.mu.Unlock()
		return nil, io.EOF
	}
	r := m.AcceptStreams[min(m.acceptIdx, len(m.AcceptStreams)-1)]
	m.acceptIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *FakeStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	m.mu.Lock()
	signal(m.AcceptNotify)
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.AcceptUniStreams) == 0 {
		m.mu.Unlock()
		return nil, io.EOF
	}
	r := m.AcceptUniStreams[min(m.acceptUniIdx, len(m.AcceptUniStreams)-1)]
	m.acceptUniIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *FakeStreamConn) OpenStream() (transport.Stream, error) {
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.OpenStreams) == 0 {
		m.mu.Unlock()
		return nil, nil
	}
	r := m.OpenStreams[min(m.openIdx, len(m.OpenStreams)-1)]
	m.openIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *FakeStreamConn) OpenUniStream() (transport.SendStream, error) {
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.OpenUniStreams) == 0 {
		m.mu.Unlock()
		return nil, nil
	}
	r := m.OpenUniStreams[min(m.openUniIdx, len(m.OpenUniStreams)-1)]
	m.openUniIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

// OpenStreamSync models the blocking open as the non-blocking one, so tests
// that populate OpenStreams cover production paths calling either form.
func (m *FakeStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	return m.OpenStream()
}

func (m *FakeStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	return m.OpenUniStream()
}

func (m *FakeStreamConn) LocalAddr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.LocalAddrValue != nil {
		return m.LocalAddrValue
	}
	return &net.TCPAddr{}
}

func (m *FakeStreamConn) RemoteAddr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.RemoteAddrValue != nil {
		return m.RemoteAddrValue
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// CloseWithError models quic-go behavior:
//   - First call stores the close error and cancels Context() with *transport.ApplicationError as cause
//   - Subsequent calls are no-ops
//   - Returns nil (like quic-go) unless CloseErr is set
func (m *FakeStreamConn) CloseWithError(code transport.ConnErrorCode, reason string) error {
	call := closeCall{Code: code, Reason: reason}

	m.mu.Lock()
	m.closeCalls = append(m.closeCalls, call)
	notify := m.CloseNotify
	if m.closeErr != nil {
		// Already closed — first-writer-wins, idempotent
		closeErr := m.CloseErr
		m.mu.Unlock()
		if notify != nil {
			select {
			case notify <- call:
			default:
			}
		}
		return closeErr
	}
	m.closeErr = &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(code),
		ErrorMessage: reason,
	}
	m.ensureContext()
	cancel := m.cancelCause
	cause := m.closeErr
	closeErr := m.CloseErr
	m.mu.Unlock()

	cancel(cause)
	if notify != nil {
		select {
		case notify <- call:
		default:
		}
	}
	return closeErr
}

// CloseCalls returns the CloseWithError invocations, in order.
func (m *FakeStreamConn) CloseCalls() []closeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]closeCall, len(m.closeCalls))
	copy(out, m.closeCalls)
	return out
}

func (m *FakeStreamConn) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContext()
	return m.ctx
}

func (m *FakeStreamConn) ConnectionStats() quicgo.ConnectionStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Stats.BytesSent += m.StatsBytesSentStep
	return m.Stats
}

type FakeWebTransportSession struct {
	FakeStreamConn
	SubprotocolValue string
}

func (m *FakeWebTransportSession) Subprotocol() string {
	return m.SubprotocolValue
}
