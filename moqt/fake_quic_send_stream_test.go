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
type FakeQUICSendStream struct {
	mu sync.Mutex

	WriteFunc            func(p []byte) (int, error)
	CloseFunc            func() error
	CancelWriteFunc      func(transport.StreamErrorCode)
	ParentCtx            context.Context // optional parent context
	SetWriteDeadlineFunc func(time.Time) error

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
	if m.WriteFunc != nil {
		return m.WriteFunc(p)
	}
	return len(p), nil
}

func (m *FakeQUICSendStream) CancelWrite(code transport.StreamErrorCode) {
	if m.CancelWriteFunc != nil {
		m.CancelWriteFunc(code)
		return
	}
	m.mu.Lock()
	if m.closed || m.cancelWriteErr != nil {
		m.mu.Unlock()
		return
	}
	m.cancelWriteErr = &transport.StreamError{ErrorCode: code}
	m.ensureContext()
	cancel := m.cancelCause
	m.mu.Unlock()
	cancel(m.cancelWriteErr)
}

func (m *FakeQUICSendStream) SetWriteDeadline(t time.Time) error {
	if m.SetWriteDeadlineFunc != nil {
		return m.SetWriteDeadlineFunc(t)
	}
	return nil
}

func (m *FakeQUICSendStream) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	m.mu.Lock()
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
