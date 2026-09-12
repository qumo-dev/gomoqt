package moqt

import (
	"io"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.ReceiveStream = (*FakeQUICReceiveStream)(nil)

// FakeQUICReceiveStream is a fake implementation of ReceiveStream for testing.
// Models quic-go behavior: CancelRead makes subsequent Read return *transport.StreamError.
//
// Read behavior is driven by the Reads results queue: entries are returned in
// order and the last entry repeats once exhausted; an empty queue returns
// io.EOF. A Block entry parks the read until CancelRead.
type FakeQUICReceiveStream struct {
	mu sync.Mutex

	Reads []streamResult

	// ReadFrom backs the stream with a real source, for cases a finite queue
	// cannot express — typically an endless generator in a benchmark. It
	// applies only once the Reads queue is exhausted.
	ReadFrom io.Reader

	SetReadDeadlineErr error

	// ReadGate, when set, releases a read parked on a Block entry.
	ReadGate <-chan struct{}

	CancelReadNotify chan<- transport.StreamErrorCode
	// DrainNotify is signalled once every queued read has been consumed.
	DrainNotify chan<- struct{}

	reads           resultQueue
	cancelReadCodes []transport.StreamErrorCode
	unblock         chan struct{}

	cancelReadErr error
}

// ensureUnblock lazily creates the channel that releases blocked reads.
// Must be called with m.mu held.
func (m *FakeQUICReceiveStream) ensureUnblock() {
	if m.unblock == nil {
		m.unblock = make(chan struct{})
	}
}

func (m *FakeQUICReceiveStream) Read(p []byte) (int, error) {
	m.mu.Lock()
	if m.cancelReadErr != nil {
		err := m.cancelReadErr
		m.mu.Unlock()
		return 0, err
	}
	if m.reads.entries == nil && m.Reads != nil {
		m.reads.entries = m.Reads
	}
	if m.reads.blocking() {
		m.ensureUnblock()
		unblock := m.unblock
		gate := m.ReadGate
		m.mu.Unlock()

		select {
		case <-unblock:
		case <-gate:
		}

		m.mu.Lock()
		if m.cancelReadErr != nil {
			err := m.cancelReadErr
			m.mu.Unlock()
			return 0, err
		}
		// A Block entry with more behind it is a pause, not an ending.
		if m.reads.idx < len(m.reads.entries)-1 {
			m.reads.idx++
			m.reads.off = 0
			n, err := m.readLocked(p)
			m.mu.Unlock()
			return n, err
		}
		m.mu.Unlock()
		return 0, io.EOF
	}
	if len(m.reads.entries) == 0 && m.ReadFrom != nil {
		src := m.ReadFrom
		m.mu.Unlock()
		return src.Read(p)
	}
	n, err := m.readLocked(p)
	m.mu.Unlock()
	return n, err
}

// readLocked serves the queue and signals DrainNotify once it is exhausted.
// Must be called with m.mu held.
func (m *FakeQUICReceiveStream) readLocked(p []byte) (int, error) {
	n, err := m.reads.readInto(p)
	if m.reads.idx >= len(m.reads.entries) {
		signal(m.DrainNotify)
	}
	return n, err
}

func (m *FakeQUICReceiveStream) CancelRead(code transport.StreamErrorCode) {
	m.mu.Lock()
	m.cancelReadCodes = append(m.cancelReadCodes, code)
	if m.cancelReadErr == nil {
		m.cancelReadErr = &transport.StreamError{ErrorCode: code}
	}
	m.ensureUnblock()
	select {
	case <-m.unblock:
	default:
		close(m.unblock)
	}
	notify := m.CancelReadNotify
	m.mu.Unlock()

	if notify != nil {
		select {
		case notify <- code:
		default:
		}
	}
}

// CancelReadCodes returns the codes passed to CancelRead, in order.
func (m *FakeQUICReceiveStream) CancelReadCodes() []transport.StreamErrorCode {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(m.cancelReadCodes))
	copy(out, m.cancelReadCodes)
	return out
}

func (m *FakeQUICReceiveStream) SetReadDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SetReadDeadlineErr
}
