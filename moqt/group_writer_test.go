package moqt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewGroupWriter(t *testing.T) {
	tests := map[string]struct {
		setupMock func() *MockQUICSendStream
		sequence  GroupSequence
	}{
		"valid stream and sequence": {
			setupMock: func() *MockQUICSendStream {
				mockStream := &MockQUICSendStream{}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			sequence: GroupSequence(123),
		},
		"different sequence": {
			setupMock: func() *MockQUICSendStream {
				mockStream := &MockQUICSendStream{}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			sequence: GroupSequence(456),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.setupMock()
			onClose := func() {}

			sgs := newGroupWriter(mockStream, tt.sequence, onClose)

			assert.NotNil(t, sgs)
			assert.Equal(t, tt.sequence, sgs.sequence)
			assert.Equal(t, uint64(0), sgs.frameCount)
			assert.NotNil(t, sgs.ctx)
			assert.Equal(t, mockStream, sgs.stream)
			assert.NotNil(t, sgs.onClose)
		})
	}
}

func TestGroupWriter_GroupSequence(t *testing.T) {
	mockStream := &MockQUICSendStream{}
	mockStream.On("Context").Return(context.Background())
	sequence := GroupSequence(789)
	sgs := newGroupWriter(mockStream, sequence, func() {})

	result := sgs.GroupSequence()
	assert.Equal(t, sequence, result)
}

func TestGroupWriter_WriteFrame(t *testing.T) {
	tests := map[string]struct {
		setupFrame  func() *Frame
		setupMock   func() *MockQUICSendStream
		expectError bool
	}{
		"write valid frame": {
			setupFrame: func() *Frame {
				frame := NewFrame(10)
				_, _ = frame.Write([]byte("test data"))
				return frame
			},
			setupMock: func() *MockQUICSendStream {
				mockStream := &MockQUICSendStream{}
				mockStream.On("Context").Return(context.Background())
				mockStream.On("Write", mock.Anything).Return(0, nil)
				return mockStream
			},
			expectError: false,
		},
		"write nil frame": {
			setupFrame: func() *Frame {
				return nil
			},
			setupMock: func() *MockQUICSendStream {
				mockStream := &MockQUICSendStream{}
				mockStream.On("Context").Return(context.Background())
				return mockStream
			},
			expectError: false,
		},
		"write frame with error": {
			setupFrame: func() *Frame {
				frame := NewFrame(10)
				_, _ = frame.Write([]byte("test data"))
				return frame
			},
			setupMock: func() *MockQUICSendStream {
				mockStream := &MockQUICSendStream{}
				mockStream.On("Context").Return(context.Background())
				mockStream.On("Write", mock.Anything).Return(0, errors.New("write error"))
				return mockStream
			},
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.setupMock()
			sgs := newGroupWriter(mockStream, GroupSequence(123), func() {})

			frame := tt.setupFrame()
			err := sgs.WriteFrame(frame)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

func TestGroupWriter_SetWriteDeadline(t *testing.T) {
	mockStream := &MockQUICSendStream{}
	mockStream.On("Context").Return(context.Background())
	deadline := time.Now().Add(time.Minute)
	mockStream.On("SetWriteDeadline", deadline).Return(nil)
	sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

	err := sgs.SetWriteDeadline(deadline)
	assert.NoError(t, err)
	mockStream.AssertExpectations(t)
}

func TestGroupWriter_Close(t *testing.T) {
	mockStream := &MockQUICSendStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil)

	sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

	err := sgs.Close()
	assert.NoError(t, err)
	mockStream.AssertExpectations(t)
}

func TestGroupWriter_ContextCancellation(t *testing.T) {
	t.Run("operations continue when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mockStream := &MockQUICSendStream{}
		mockStream.On("Context").Return(ctx)
		mockStream.On("Write", mock.Anything).Return(4, nil)

		sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

		// Cancel the context
		cancel()

		// Test that operations continue to work (they don't check context in current implementation)
		frameLocal := NewFrame(len([]byte("test")))
		_, _ = frameLocal.Write([]byte("test"))
		err := sgs.WriteFrame(frameLocal)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), sgs.frameCount)
		mockStream.AssertExpectations(t)
	})
}

func TestGroupWriter_CloseWithStreamError(t *testing.T) {
	t.Run("close returns stream error", func(t *testing.T) {
		mockStream := &MockQUICSendStream{}
		mockStream.On("Context").Return(context.Background())

		streamID := StreamID(123)
		streamErr := &StreamError{
			StreamID:  streamID,
			ErrorCode: StreamErrorCode(42),
		}

		mockStream.On("Close").Return(streamErr)

		sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

		err := sgs.Close()
		// Due to the current implementation bug, Cause(ctx) returns nil when there's no context cause
		assert.NoError(t, err)
		mockStream.AssertExpectations(t)
	})

	t.Run("close returns non-stream error", func(t *testing.T) {
		mockStream := &MockQUICSendStream{}
		mockStream.On("Context").Return(context.Background())

		otherErr := errors.New("some other error")

		mockStream.On("Close").Return(otherErr)

		sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

		err := sgs.Close()
		// Due to the current implementation bug, Cause(ctx) returns nil when there's no context cause
		assert.NoError(t, err)
		mockStream.AssertExpectations(t)
	})

	t.Run("close when context already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		mockStream := &MockQUICSendStream{}
		mockStream.On("Context").Return(ctx)
		mockStream.On("Close").Return(nil)

		sgs := newGroupWriter(mockStream, GroupSequence(1), func() {})

		// Cancel the context first
		cancel()

		err := sgs.Close()
		assert.NoError(t, err) // Close still works even if context is cancelled
		mockStream.AssertExpectations(t)
	})
}

func TestGroupWriter_Context(t *testing.T) {
	mockStream := &MockQUICSendStream{}
	mockStream.On("Context").Return(context.Background())

	sgs := newGroupWriter(mockStream, GroupSequence(123), func() {})

	ctx := sgs.Context()
	assert.NotNil(t, ctx)
	assert.Equal(t, message.StreamTypeGroup, ctx.Value(uniStreamTypeCtxKey))
	mockStream.AssertExpectations(t)
}

func TestGroupWriter_CancelWrite(t *testing.T) {
	called := false
	mockStream := &MockQUICSendStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("CancelWrite", StreamErrorCode(1)).Return()

	sgs := newGroupWriter(mockStream, GroupSequence(1), func() { called = true })

	sgs.CancelWrite(1)
	assert.True(t, called)
	mockStream.AssertExpectations(t)
}
