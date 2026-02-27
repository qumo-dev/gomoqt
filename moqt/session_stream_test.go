package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestNewSessionStream tests basic SessionStream creation
func TestNewSessionStream(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()

	ss := newSessionStream(mockStream)

	assert.NotNil(t, ss, "newSessionStream should not return nil")
	assert.NotNil(t, ss.Updated(), "SessionUpdated channel should be initialized")

	mockStream.AssertExpectations(t)
}

// TestSessionStream_updateSession tests basic session update functionality
func TestSessionStream_updateSession(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Write", mock.Anything).Return(8, nil)

	ss := newSessionStream(mockStream)

	bitrate := uint64(1000000)
	err := ss.updateSession(bitrate)

	assert.NoError(t, err, "updateSession should not return error")
	assert.Equal(t, bitrate, ss.localBitrate, "local bitrate should be updated")

	mockStream.AssertCalled(t, "Write", mock.Anything)
	mockStream.AssertExpectations(t)
}

// TestSessionStream_updateSession_WriteError tests behavior on write errors
func TestSessionStream_updateSession_WriteError(t *testing.T) {
	mockStream := &MockQUICStream{}
	writeError := errors.New("write error")
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Write", mock.Anything).Return(0, writeError)

	ss := newSessionStream(mockStream)

	assert.NotPanics(t, func() {
		_ = ss.updateSession(uint64(1000000))
	}, "updateSession should not panic on write error")

	mockStream.AssertExpectations(t)
}

// TestSessionStream_SessionUpdated tests SessionUpdated channel functionality
func TestSessionStream_SessionUpdated(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()

	ss := newSessionStream(mockStream)

	ss.handleUpdates()

	ch := ss.Updated()
	assert.NotNil(t, ch, "SessionUpdated should return a valid channel")
	assert.IsType(t, (<-chan struct{})(nil), ch, "SessionUpdated should return a receive-only channel")

	mockStream.AssertExpectations(t)
}

// TestSessionStream_updateSession_ZeroBitrate tests updateSession with zero bitrate
func TestSessionStream_updateSession_ZeroBitrate(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Write", mock.Anything).Return(2, nil)

	ss := newSessionStream(mockStream)

	err := ss.updateSession(0)
	assert.NoError(t, err, "updateSession(0) should not error")
	assert.Equal(t, uint64(0), ss.localBitrate, "local bitrate should be set to 0")

	mockStream.AssertCalled(t, "Write", mock.Anything)
	mockStream.AssertExpectations(t)
}

// TestSessionStream_updateSession_LargeBitrate tests updateSession with large bitrate values
func TestSessionStream_updateSession_LargeBitrate(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Write", mock.Anything).Return(10, nil)

	ss := newSessionStream(mockStream)

	largeBitrate := uint64(1<<62 - 1)
	err := ss.updateSession(largeBitrate)
	assert.NoError(t, err)
	assert.Equal(t, largeBitrate, ss.localBitrate, "local bitrate should be set correctly")

	mockStream.AssertCalled(t, "Write", mock.Anything)
	mockStream.AssertExpectations(t)
}

// TestSessionStream_listenUpdates tests message listening functionality
func TestSessionStream_listenUpdates(t *testing.T) {
	tests := map[string]struct {
		mockStream    func() *MockQUICStream
		expectUpdate  bool
		expectBitrate uint64
	}{
		"valid message": {
			mockStream: func() *MockQUICStream {
				bitrate := uint64(1000000)
				var buf bytes.Buffer
				_ = (message.SessionUpdateMessage{Bitrate: bitrate}).Encode(&buf)

				mockStream := &MockQUICStream{ReadFunc: buf.Read}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			expectUpdate:  true,
			expectBitrate: 1000000,
		},
		"empty stream": {
			mockStream: func() *MockQUICStream {
				var buf bytes.Buffer
				mockStream := &MockQUICStream{ReadFunc: buf.Read}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			expectUpdate:  false,
			expectBitrate: 0,
		},
		"zero bitrate": {
			mockStream: func() *MockQUICStream {
				var buf bytes.Buffer
				_ = (message.SessionUpdateMessage{Bitrate: 0}).Encode(&buf)

				mockStream := &MockQUICStream{ReadFunc: buf.Read}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			expectUpdate:  true,
			expectBitrate: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.mockStream()

			ss := newSessionStream(mockStream)
			ss.handleUpdates()
			// capture channel early; it may become nil later
			ch := ss.Updated()
			readCh := make(chan struct{}, 1)
			if mockStream.ReadFunc != nil {
				origRead := mockStream.ReadFunc
				mockStream.ReadFunc = func(p []byte) (int, error) {
					n, err := origRead(p)
					select {
					case readCh <- struct{}{}:
					default:
					}
					return n, err
				}
			}
			select {
			case <-readCh:
			case <-time.After(200 * time.Millisecond):
				t.Fatal("listenUpdates did not perform read")
			}

			if tt.expectUpdate {
				// only wait on channel if it wasn't nil when captured
				if ch != nil {
					select {
					case <-ch:
					case <-time.After(500 * time.Millisecond):
						if name == "valid message" || name == "zero bitrate" {
							t.Error("expected session update but timed out")
						}
					}
				}
				ss.mu.Lock()
				actual := ss.remoteBitrate
				ss.mu.Unlock()
				assert.Equal(t, tt.expectBitrate, actual)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

// TestSessionStream_listenUpdates_StreamClosed tests behavior when stream is closed
func TestSessionStream_listenUpdates_StreamClosed(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()

	ss := newSessionStream(mockStream)
	ss.handleUpdates()

	select {
	case <-ss.Updated():
	case <-time.After(50 * time.Millisecond):
	}

	mockStream.AssertExpectations(t)
}

// TestSessionStream_listenUpdates_ContextCancellation tests behavior on context cancellation
func TestSessionStream_listenUpdates_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(ctx)
	mockStream.On("Read", mock.Anything).Return(0, nil).Maybe()

	ss := newSessionStream(mockStream)
	cancel()

	select {
	case <-ss.Updated():
	case <-time.After(50 * time.Millisecond):
	}

	mockStream.AssertExpectations(t)
}

// TestSessionStream_ConcurrentAccess tests concurrent access to SessionStream methods
func TestSessionStream_ConcurrentAccess(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Write", mock.Anything).Return(8, nil).Maybe()

	ss := newSessionStream(mockStream)
	ss.handleUpdates()

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range 5 {
			_ = ss.updateSession(uint64(i * 1000))
			time.Sleep(time.Millisecond)
		}
	})

	wg.Go(func() {
		for range 5 {
			ss.Updated()
			time.Sleep(time.Millisecond)
		}
	})

	wg.Go(func() {
		for range 5 {
			_ = ss.localBitrate
			_ = ss.remoteBitrate
			time.Sleep(time.Millisecond)
		}
	})

	wg.Wait()
	mockStream.AssertExpectations(t)
}

// TestSessionStream_handleUpdatesAndClose ensures calling closeWithError after
// handleUpdates has already closed the updatedCh channel is safe and leaves
// the reference nil.
func TestSessionStream_handleUpdatesAndClose(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()

	// mock stream may have cancellation invoked when closeWithError runs
	mockStream.On("CancelRead", quic.StreamErrorCode(SessionErrorCode(1))).Return()
	mockStream.On("CancelWrite", quic.StreamErrorCode(SessionErrorCode(1))).Return()

	ss := newSessionStream(mockStream)
	ss.handleUpdates()

	// give goroutine a moment to observe EOF and close channel
	time.Sleep(20 * time.Millisecond)

	// should not panic even if updatedCh was closed by handleUpdates
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic during closeWithError: %v", r)
		}
	}()
	ss.closeWithError(SessionErrorCode(1))

	if ch := ss.Updated(); ch != nil {
		t.Errorf("expected updatedCh nil after close, got %v", ch)
	}

	mockStream.AssertExpectations(t)
}

// TestSessionStream_listenUpdates_InitialChannelState tests initial state of updatedCh
func TestSessionStream_listenUpdates_InitialChannelState(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()

	ss := newSessionStream(mockStream)
	ss.handleUpdates()

	ch := ss.Updated()
	assert.NotNil(t, ch, "SessionUpdated channel should be initialized")

	mockStream.AssertExpectations(t)
}

// TestSessionStream_Context tests Context method
func TestSessionStream_Context(t *testing.T) {
	ctx := context.Background()
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(ctx)

	ss := newSessionStream(mockStream)

	resultCtx := ss.Context()
	assert.NotNil(t, resultCtx, "Context should not be nil")
	assert.NotEqual(t, ctx, resultCtx, "Context should be derived with additional values")
}

// TestSessionStream_Reject tests session rejection functionality
func TestSessionStream_Reject(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("CancelWrite", quic.StreamErrorCode(SessionErrorCode(1))).Return()
	mockStream.On("CancelRead", quic.StreamErrorCode(SessionErrorCode(1))).Return()

	mockConn := &MockQUICConnection{}
	mockConn.On("Context").Return(context.Background())

	sessStr := &sessionStream{
		stream: mockStream,
	}
	resp := &response{sessionStream: sessStr}
	w := newResponseWriter(mockConn, resp, nil, nil) // no client versions in this test
	w.Reject(SessionErrorCode(1))

	mockStream.AssertExpectations(t)
}
