package moqt

import (
	"io"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.ReceiveStream = (*fakeQUICReceiveStream)(nil)

// fakeQUICReceiveStream is a fake implementation of ReceiveStream for testing.
// Models quic-go behavior: CancelRead makes subsequent Read return *transport.StreamError.
//
// Read behavior is driven by the Reads results queue: entries are returned in
// order and the last entry repeats once exhausted; an empty queue returns
// io.EOF. A Block entry parks the read until CancelRead.
type fakeQUICReceiveStream struct {
	mu sync.Mutex

	reads []streamResult

	// readFrom backs the stream with a real source, for cases a finite queue
	// cannot express — typically an endless generator in a benchmark. It
	// applies only when the Reads queue is empty; an explicit queue wins.
	readFrom io.Reader

	setReadDeadlineErr error

	// readGate, when set, releases a read parked on a Block entry.
	readGate <-chan struct{}

	cancelReadNotify chan<- transport.StreamErrorCode
	// drainNotify is signalled once every queued read has been consumed.
	drainNotify chan<- struct{}

	readQueue       resultQueue
	cancelReadCodes []transport.StreamErrorCode
	unblock         chan struct{}

	cancelReadErr error
}

// ensureUnblock lazily creates the channel that releases blocked reads.
// Must be called with m.mu held.
func (m *fakeQUICReceiveStream) ensureUnblock() {
	if m.unblock == nil {
		m.unblock = make(chan struct{})
	}
}

func (m *fakeQUICReceiveStream) Read(p []byte) (int, error) {
	m.mu.Lock()
	if m.cancelReadErr != nil {
		err := m.cancelReadErr
		m.mu.Unlock()
		return 0, err
	}
	if m.readQueue.entries == nil && m.reads != nil {
		m.readQueue.entries = m.reads
	}
	if m.readQueue.blocking() {
		m.ensureUnblock()
		unblock := m.unblock
		gate := m.readGate
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
		if m.readQueue.idx < len(m.readQueue.entries)-1 {
			m.readQueue.idx++
			m.readQueue.off = 0
			n, err := m.readLocked(p)
			m.mu.Unlock()
			return n, err
		}
		m.mu.Unlock()
		return 0, io.EOF
	}
	if len(m.readQueue.entries) == 0 && m.readFrom != nil {
		src := m.readFrom
		m.mu.Unlock()
		return src.Read(p)
	}
	n, err := m.readLocked(p)
	m.mu.Unlock()
	return n, err
}

// readLocked serves the queue and signals DrainNotify once it is exhausted.
// Must be called with m.mu held.
func (m *fakeQUICReceiveStream) readLocked(p []byte) (int, error) {
	n, err := m.readQueue.readInto(p)
	if m.readQueue.drained() {
		signal(m.drainNotify)
	}
	return n, err
}

func (m *fakeQUICReceiveStream) CancelRead(code transport.StreamErrorCode) {
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
	notify := m.cancelReadNotify
	m.mu.Unlock()

	if notify != nil {
		select {
		case notify <- code:
		default:
		}
	}
}

// CancelReadCodes returns the codes passed to CancelRead, in order.
func (m *fakeQUICReceiveStream) CancelReadCodes() []transport.StreamErrorCode {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(m.cancelReadCodes))
	copy(out, m.cancelReadCodes)
	return out
}

func (m *fakeQUICReceiveStream) SetReadDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.setReadDeadlineErr
}
