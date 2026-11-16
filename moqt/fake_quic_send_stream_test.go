package moqt

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.SendStream = (*fakeQUICSendStream)(nil)

// fakeQUICSendStream is a fake implementation of SendStream for testing.
// Models quic-go behavior: Close and CancelWrite cancel Context().
//
// Write behavior is driven by the Writes results queue: entries are returned in
// order and the last entry repeats once exhausted; an empty queue means every
// write succeeds. Bytes passed to a successful Write are recorded and readable
// via Written.
type fakeQUICSendStream struct {
	mu sync.Mutex

	writes []streamResult

	// writeTo backs the stream with a real sink, for cases a finite queue
	// cannot express — typically a discard sink in a benchmark. It applies
	// only when the Writes queue is empty; an explicit queue wins over the
	// sink. Bytes routed to writeTo are NOT also recorded for Written, so a
	// benchmark sink does not grow an ever-larger capture buffer; use Written
	// or writeTo, not both.
	writeTo io.Writer

	parentCtx context.Context // optional parent context

	// Error overrides; zero value means the call succeeds.
	closeErr            error
	setWriteDeadlineErr error

	// Notification channels, for tests that must observe a call as it happens.
	// Each send is non-blocking, so an unbuffered channel with no reader is safe.
	writeNotify       chan<- struct{}
	cancelWriteNotify chan<- transport.StreamErrorCode

	writeQueue       resultQueue
	written          []byte
	cancelWriteCodes []transport.StreamErrorCode

	ctx            context.Context
	cancelCause    context.CancelCauseFunc
	closed         bool  // true after Close
	cancelWriteErr error // non-nil after CancelWrite

	prioritySet         bool
	priorityUrgency     int8
	priorityIncremental bool
}

func (m *fakeQUICSendStream) ensureContext() {
	if m.ctx == nil {
		parent := m.parentCtx
		if parent == nil {
			parent = context.Background()
		}
		m.ctx, m.cancelCause = context.WithCancelCause(parent)
	}
}

func (m *fakeQUICSendStream) Write(p []byte) (int, error) {
	m.mu.Lock()
	if m.writeQueue.entries == nil && m.writes != nil {
		m.writeQueue.entries = m.writes
	}
	err := m.writeQueue.advance()
	sink := m.writeTo
	if len(m.writeQueue.entries) > 0 {
		sink = nil // an explicit queue result wins over the sink
	}
	if err == nil && sink == nil {
		// A sink owns the bytes; recording them too would make every
		// discard-backed benchmark grow an unbounded capture buffer.
		m.written = append(m.written, p...)
	}
	notify := m.writeNotify
	m.mu.Unlock()

	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	if err != nil {
		return 0, err
	}
	if sink != nil {
		return sink.Write(p)
	}
	return len(p), nil
}

// Written returns a copy of every byte passed to a successful Write.
func (m *fakeQUICSendStream) Written() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.written))
	copy(out, m.written)
	return out
}

func (m *fakeQUICSendStream) CancelWrite(code transport.StreamErrorCode) {
	m.mu.Lock()
	m.cancelWriteCodes = append(m.cancelWriteCodes, code)
	if m.closed || m.cancelWriteErr != nil {
		notify := m.cancelWriteNotify
		m.mu.Unlock()
		if notify != nil {
			select {
			case notify <- code:
			default:
			}
		}
		return
	}
	m.cancelWriteErr = &transport.StreamError{ErrorCode: code}
	m.ensureContext()
	cancel := m.cancelCause
	notify := m.cancelWriteNotify
	m.mu.Unlock()

	cancel(m.cancelWriteErr)
	if notify != nil {
		select {
		case notify <- code:
		default:
		}
	}
}

// CancelWriteCodes returns the codes passed to CancelWrite, in order.
func (m *fakeQUICSendStream) CancelWriteCodes() []transport.StreamErrorCode {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(m.cancelWriteCodes))
	copy(out, m.cancelWriteCodes)
	return out
}

func (m *fakeQUICSendStream) SetWriteDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.setWriteDeadlineErr
}

func (m *fakeQUICSendStream) Close() error {
	m.mu.Lock()
	if m.closeErr != nil {
		err := m.closeErr
		m.mu.Unlock()
		return err
	}
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	cancelled := m.cancelWriteErr != nil
	m.ensureContext()
	cancel := m.cancelCause
	m.mu.Unlock()
	if cancelled {
		return fmt.Errorf("close called for canceled stream")
	}
	cancel(nil)
	return nil
}

func (m *fakeQUICSendStream) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContext()
	return m.ctx
}

func (m *fakeQUICSendStream) SetPriority(urgency int8, incremental bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prioritySet = true
	m.priorityUrgency = urgency
	m.priorityIncremental = incremental
}

// LastPriority reports the urgency/incremental values from the most recent
// SetPriority call, and whether SetPriority was ever called.
func (m *fakeQUICSendStream) LastPriority() (urgency int8, incremental bool, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.priorityUrgency, m.priorityIncremental, m.prioritySet
}
