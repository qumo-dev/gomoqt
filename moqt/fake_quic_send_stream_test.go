package moqt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.SendStream = (*FakeQUICSendStream)(nil)

// FakeQUICSendStream is a fake implementation of SendStream for testing.
// Models quic-go behavior: Close and CancelWrite cancel Context().
//
// Write behavior is driven by the Writes results queue: entries are returned in
// order and the last entry repeats once exhausted; an empty queue means every
// write succeeds. Bytes passed to a successful Write are recorded and readable
// via Written.
type FakeQUICSendStream struct {
	mu sync.Mutex

	Writes []streamResult

	ParentCtx context.Context // optional parent context

	// Error overrides; zero value means the call succeeds.
	CloseErr            error
	SetWriteDeadlineErr error

	// Notification channels, for tests that must observe a call as it happens.
	// Each send is non-blocking, so an unbuffered channel with no reader is safe.
	WriteNotify       chan<- struct{}
	CancelWriteNotify chan<- transport.StreamErrorCode

	writes           resultQueue
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

func (m *FakeQUICSendStream) ensureContext() {
	if m.ctx == nil {
		parent := m.ParentCtx
		if parent == nil {
			parent = context.Background()
		}
		m.ctx, m.cancelCause = context.WithCancelCause(parent)
	}
}

func (m *FakeQUICSendStream) Write(p []byte) (int, error) {
	m.mu.Lock()
	if m.writes.entries == nil && m.Writes != nil {
		m.writes.entries = m.Writes
	}
	err := m.writes.advance()
	if err == nil {
		m.written = append(m.written, p...)
	}
	notify := m.WriteNotify
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
	return len(p), nil
}

// Written returns a copy of every byte passed to a successful Write.
func (m *FakeQUICSendStream) Written() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.written))
	copy(out, m.written)
	return out
}

func (m *FakeQUICSendStream) CancelWrite(code transport.StreamErrorCode) {
	m.mu.Lock()
	m.cancelWriteCodes = append(m.cancelWriteCodes, code)
	if m.closed || m.cancelWriteErr != nil {
		notify := m.CancelWriteNotify
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
	notify := m.CancelWriteNotify
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
func (m *FakeQUICSendStream) CancelWriteCodes() []transport.StreamErrorCode {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(m.cancelWriteCodes))
	copy(out, m.cancelWriteCodes)
	return out
}

func (m *FakeQUICSendStream) SetWriteDeadline(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SetWriteDeadlineErr
}

func (m *FakeQUICSendStream) Close() error {
	m.mu.Lock()
	if m.CloseErr != nil {
		err := m.CloseErr
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

func (m *FakeQUICSendStream) Context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContext()
	return m.ctx
}

func (m *FakeQUICSendStream) SetPriority(urgency int8, incremental bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prioritySet = true
	m.priorityUrgency = urgency
	m.priorityIncremental = incremental
}

// LastPriority reports the urgency/incremental values from the most recent
// SetPriority call, and whether SetPriority was ever called.
func (m *FakeQUICSendStream) LastPriority() (urgency int8, incremental bool, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.priorityUrgency, m.priorityIncremental, m.prioritySet
}
