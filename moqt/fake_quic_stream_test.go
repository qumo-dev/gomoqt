package moqt

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

// FakeQUICStream is a fake implementation of transport.Stream for testing.
// By default it models quic-go behavior:
//   - Context() is the send-side context (cancelled by Close / CancelWrite, NOT by Read errors)
//   - Close cancels Context() with nil cause (context.Cause returns context.Canceled)
//   - CancelWrite cancels Context() with *transport.StreamError cause
//   - CancelRead makes subsequent Read calls return *transport.StreamError (does NOT cancel Context)
type FakeQUICStream struct {
	mu sync.Mutex

	ReadFunc             func(p []byte) (int, error)
	WriteFunc            func(p []byte) (int, error)
	CloseFunc            func() error
	CancelReadFunc       func(transport.StreamErrorCode)
	CancelWriteFunc      func(transport.StreamErrorCode)
	ParentCtx            context.Context // optional parent context; default: context.Background()
	SetDeadlineFunc      func(time.Time) error
	SetReadDeadlineFunc  func(time.Time) error
	SetWriteDeadlineFunc func(time.Time) error

	ctx            context.Context
	cancelCause    context.CancelCauseFunc
	cancelReadErr  error
	closed         bool  // true after Close (finishedWriting in quic-go)
	cancelWriteErr error // non-nil after CancelWrite (resetErr in quic-go)

	prioritySet         bool
	priorityUrgency     int8
	priorityIncremental bool
}

var _ transport.Stream = (*FakeQUICStream)(nil)

// ensureContext lazily initialises the internal cancellable context.
// Must be called with f.mu held.
func (f *FakeQUICStream) ensureContext() {
	if f.ctx == nil {
		parent := f.ParentCtx
		if parent == nil {
			parent = context.Background()
		}
		f.ctx, f.cancelCause = context.WithCancelCause(parent)
	}
}

func (f *FakeQUICStream) Read(p []byte) (int, error) {
	f.mu.Lock()
	if f.cancelReadErr != nil {
		err := f.cancelReadErr
		f.mu.Unlock()
		return 0, err
	}
	readFunc := f.ReadFunc
	f.mu.Unlock()
	if readFunc != nil {
		return readFunc(p)
	}
	return 0, io.EOF
}

func (f *FakeQUICStream) Write(p []byte) (int, error) {
	if f.WriteFunc != nil {
		return f.WriteFunc(p)
	}
	return len(p), nil
}

func (f *FakeQUICStream) Close() error {
	if f.CloseFunc != nil {
		return f.CloseFunc()
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	cancelled := f.cancelWriteErr != nil
	f.ensureContext()
	cancel := f.cancelCause
	f.mu.Unlock()
	if cancelled {
		return fmt.Errorf("close called for canceled stream")
	}
	cancel(nil)
	return nil
}

func (f *FakeQUICStream) CancelRead(code transport.StreamErrorCode) {
	if f.CancelReadFunc != nil {
		f.CancelReadFunc(code)
		return
	}
	f.mu.Lock()
	if f.cancelReadErr == nil {
		f.cancelReadErr = &transport.StreamError{ErrorCode: code}
	}
	f.mu.Unlock()
}

func (f *FakeQUICStream) CancelWrite(code transport.StreamErrorCode) {
	if f.CancelWriteFunc != nil {
		f.CancelWriteFunc(code)
		return
	}
	f.mu.Lock()
	if f.closed || f.cancelWriteErr != nil {
		f.mu.Unlock()
		return
	}
	f.cancelWriteErr = &transport.StreamError{ErrorCode: code}
	f.ensureContext()
	cancel := f.cancelCause
	f.mu.Unlock()
	cancel(f.cancelWriteErr)
}

func (f *FakeQUICStream) Context() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureContext()
	return f.ctx
}

func (f *FakeQUICStream) SetDeadline(t time.Time) error {
	if f.SetDeadlineFunc != nil {
		return f.SetDeadlineFunc(t)
	}
	return nil
}

func (f *FakeQUICStream) SetReadDeadline(t time.Time) error {
	if f.SetReadDeadlineFunc != nil {
		return f.SetReadDeadlineFunc(t)
	}
	return nil
}

func (f *FakeQUICStream) SetWriteDeadline(t time.Time) error {
	if f.SetWriteDeadlineFunc != nil {
		return f.SetWriteDeadlineFunc(t)
	}
	return nil
}

func (f *FakeQUICStream) SetPriority(urgency int8, incremental bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prioritySet = true
	f.priorityUrgency = urgency
	f.priorityIncremental = incremental
}

// LastPriority reports the urgency/incremental values from the most recent
// SetPriority call, and whether SetPriority was ever called.
func (f *FakeQUICStream) LastPriority() (urgency int8, incremental bool, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.priorityUrgency, f.priorityIncremental, f.prioritySet
}
