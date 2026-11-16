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

var (
	_ StreamConn                    = (*fakeStreamConn)(nil)
	_ transport.WebTransportSession = (*fakeWebTransportSession)(nil)
)

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

// fakeStreamConn is a fake implementation of StreamConn that models quic-go
// connection semantics:
//   - CloseWithError cancels Context() with *transport.ApplicationError as cause (first-writer-wins, idempotent, always returns nil)
//   - After CloseWithError, AcceptStream/AcceptUniStream/OpenStream/OpenUniStream/OpenStreamSync/OpenUniStreamSync return the close error
//   - Context() returns a context derived from ParentCtx (default: context.Background())
//
// Stream open/accept behavior is driven by results queues: entries are returned
// in order and the last entry repeats once exhausted. An empty queue returns
// io.EOF for accepts and a nil stream for opens.
type fakeStreamConn struct {
	mu sync.Mutex

	acceptStreams    []biStreamResult
	acceptUniStreams []recvStreamResult
	openStreams      []biStreamResult
	openUniStreams   []sendStreamResult

	parentCtx context.Context

	// Value overrides; the zero value yields the documented default.
	localAddrValue  net.Addr
	remoteAddrValue net.Addr
	tlsState        *tls.ConnectionState
	stats           quicgo.ConnectionStats
	closeErr        error // returned by CloseWithError instead of nil

	// statsBytesSentStep advances the reported BytesSent by this much on every
	// ConnectionStats call, modelling a connection with ongoing traffic.
	statsBytesSentStep uint64

	// Notification channels; sends are non-blocking.
	closeNotify  chan<- closeCall
	acceptNotify chan<- struct{}

	acceptIdx    int
	acceptUniIdx int
	openIdx      int
	openUniIdx   int

	openCalls  int
	statsCalls int
	closeCalls []closeCall

	ctx         context.Context
	cancelCause context.CancelCauseFunc
	closedErr   error // set by first CloseWithError call
}

// ensureContext lazily initialises the internal cancellable context.
// Must be called with m.mu held.
func (m *fakeStreamConn) ensureContext() {
	if m.ctx == nil {
		parent := m.parentCtx
		if parent == nil {
			parent = context.Background()
		}
		m.ctx, m.cancelCause = context.WithCancelCause(parent)
	}
}

func (m *fakeStreamConn) TLS() *tls.ConnectionState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tlsState
}

// waitDone parks until the connection context is done, then reports the reason.
func (m *fakeStreamConn) waitDone() error {
	m.mu.Lock()
	m.ensureContext()
	ctx := m.ctx
	m.mu.Unlock()

	<-ctx.Done()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closedErr != nil {
		return m.closedErr
	}
	return context.Cause(ctx)
}

func (m *fakeStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	m.mu.Lock()
	signal(m.acceptNotify)
	if m.closedErr != nil {
		err := m.closedErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.acceptStreams) == 0 {
		m.mu.Unlock()
		return nil, io.EOF
	}
	r := m.acceptStreams[min(m.acceptIdx, len(m.acceptStreams)-1)]
	m.acceptIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *fakeStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	m.mu.Lock()
	signal(m.acceptNotify)
	if m.closedErr != nil {
		err := m.closedErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.acceptUniStreams) == 0 {
		m.mu.Unlock()
		return nil, io.EOF
	}
	r := m.acceptUniStreams[min(m.acceptUniIdx, len(m.acceptUniStreams)-1)]
	m.acceptUniIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *fakeStreamConn) OpenStream() (transport.Stream, error) {
	m.mu.Lock()
	m.openCalls++
	if m.closedErr != nil {
		err := m.closedErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.openStreams) == 0 {
		m.mu.Unlock()
		return nil, nil
	}
	r := m.openStreams[min(m.openIdx, len(m.openStreams)-1)]
	m.openIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

func (m *fakeStreamConn) OpenUniStream() (transport.SendStream, error) {
	m.mu.Lock()
	if m.closedErr != nil {
		err := m.closedErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.openUniStreams) == 0 {
		m.mu.Unlock()
		return nil, nil
	}
	r := m.openUniStreams[min(m.openUniIdx, len(m.openUniStreams)-1)]
	m.openUniIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	return r.Stream, r.Err
}

// OpenStreamSync models the blocking open as the non-blocking one, so tests
// that populate OpenStreams cover production paths calling either form.
func (m *fakeStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	return m.OpenStream()
}

func (m *fakeStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	m.mu.Lock()
	if m.closedErr != nil {
		err := m.closedErr
		m.mu.Unlock()
		return nil, err
	}
	if len(m.openUniStreams) == 0 {
		m.mu.Unlock()
		return &fakeQUICSendStream{}, nil
	}
	r := m.openUniStreams[min(m.openUniIdx, len(m.openUniStreams)-1)]
	m.openUniIdx++
	m.mu.Unlock()

	if r.Block {
		return nil, m.waitDone()
	}
	if r.Stream == nil && r.Err == nil {
		return &fakeQUICSendStream{}, nil
	}
	return r.Stream, r.Err
}

func (m *fakeStreamConn) LocalAddr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localAddrValue != nil {
		return m.localAddrValue
	}
	return &net.TCPAddr{}
}

func (m *fakeStreamConn) RemoteAddr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remoteAddrValue != nil {
		return m.remoteAddrValue
	}
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// CloseWithError models quic-go behavior:
//   - First call stores the close error and cancels Context() with *transport.ApplicationError as cause
//   - Subsequent calls are no-ops
//   - Returns nil (like quic-go) unless CloseErr is set
func (m *fakeStreamConn) CloseWithError(code transport.ConnErrorCode, reason string) error {
	call := closeCall{Code: code, Reason: reason}

	m.mu.Lock()
	m.closeCalls = append(m.closeCalls, call)
	notify := m.closeNotify
	if m.closedErr != nil {
		// Already closed — first-writer-wins, idempotent
		closeErr := m.closeErr
		m.mu.Unlock()
		if notify != nil {
			select {
			case notify <- call:
			default:
			}
		}
		return closeErr
	}
	m.closedErr = &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(code),
		ErrorMessage: reason,
	}
	m.ensureContext()
	cancel := m.cancelCause
	cause := m.closedErr
	closeErr := m.closeErr
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

// SetBytesSent overwrites the BytesSent the connection reports, for tests that
// drive traffic growth on their own schedule rather than per sample.
func (m *fakeStreamConn) SetBytesSent(n uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats.BytesSent = n
}

// OpenCalls returns how many times a bidirectional stream open was requested.
func (m *fakeStreamConn) OpenCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openCalls
}

// CloseCalls returns the CloseWithError invocations, in order.
func (m *fakeStreamConn) CloseCalls() []closeCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]closeCall, len(m.closeCalls))
	copy(out, m.closeCalls)
	return out
}

func (m *fakeStreamConn) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContext()
	return m.ctx
}

func (m *fakeStreamConn) ConnectionStats() quicgo.ConnectionStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statsCalls++
	m.stats.BytesSent += m.statsBytesSentStep
	return m.stats
}

// StatsCalls returns how many times ConnectionStats has been sampled.
func (m *fakeStreamConn) StatsCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statsCalls
}

type fakeWebTransportSession struct {
	fakeStreamConn
	subprotocolValue string
}

func (m *fakeWebTransportSession) Subprotocol() string {
	return m.subprotocolValue
}
