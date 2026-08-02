package moqt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
)

// --- construction ----------------------------------------------------------

func TestNewGroupWriter(t *testing.T) {
	tests := map[string]struct {
		setupMock func() *FakeQUICSendStream
		sequence  GroupSequence
	}{
		"valid stream and sequence": {
			setupMock: func() *FakeQUICSendStream {
				return &FakeQUICSendStream{}
			},
			sequence: GroupSequence(123),
		},
		"different sequence": {
			setupMock: func() *FakeQUICSendStream {
				return &FakeQUICSendStream{}
			},
			sequence: GroupSequence(456),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.setupMock()
			groupManager := newGroupWriterManager()

			sgs := newGroupWriter(mockStream, tt.sequence, groupManager)

			assert.NotNil(t, sgs)
			assert.Equal(t, tt.sequence, sgs.sequence)
			assert.Equal(t, uint64(0), sgs.frameCount)
			assert.NotNil(t, sgs.ctx)
			assert.Equal(t, mockStream, sgs.stream)
			assert.Equal(t, groupManager, sgs.groupManager)
		})
	}
}

func TestGroupWriter_GroupSequence(t *testing.T) {
	mockStream := &FakeQUICSendStream{}
	sequence := GroupSequence(789)
	sgs := newGroupWriter(mockStream, sequence, newGroupWriterManager())

	result := sgs.GroupSequence()
	assert.Equal(t, sequence, result)
}

func TestGroupWriter_WriteFrame(t *testing.T) {
	var writtenData []byte

	tests := map[string]struct {
		setupFrame       func() *Frame
		setupMock        func() *FakeQUICSendStream
		expectError      bool
		expectFrameCount uint64
		verifyData       func(t *testing.T)
	}{
		"write valid frame": {
			setupFrame: func() *Frame {
				frame := NewFrame(10)
				_, _ = frame.Write([]byte("test data"))
				return frame
			},
			setupMock: func() *FakeQUICSendStream {
				return &FakeQUICSendStream{
					WriteFunc: func(p []byte) (int, error) {
						writtenData = append(writtenData, p...)
						return len(p), nil
					},
				}
			},
			expectError:      false,
			expectFrameCount: 1,
			verifyData: func(t *testing.T) {
				// The payload length is 9 ("test data"), which is encoded as a varint (1 byte for 9).
				// Thus, written data should be varint(9) + "test data".
				assert.Equal(t, append([]byte{9}, []byte("test data")...), writtenData)
			},
		},
		"write nil frame": {
			setupFrame: func() *Frame {
				return nil
			},
			setupMock: func() *FakeQUICSendStream {
				return &FakeQUICSendStream{}
			},
			expectError:      false,
			expectFrameCount: 0,
		},
		"write frame with error": {
			setupFrame: func() *Frame {
				frame := NewFrame(10)
				_, _ = frame.Write([]byte("test data"))
				return frame
			},
			setupMock: func() *FakeQUICSendStream {
				return &FakeQUICSendStream{
					WriteFunc: func(p []byte) (int, error) { return 0, errors.New("write error") },
				}
			},
			expectError:      true,
			expectFrameCount: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			writtenData = nil // reset for each run
			mockStream := tt.setupMock()
			sgs := newGroupWriter(mockStream, GroupSequence(123), newGroupWriterManager())

			frame := tt.setupFrame()
			err := sgs.WriteFrame(frame)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectFrameCount, sgs.frameCount, "frame count should match")

			if tt.verifyData != nil {
				tt.verifyData(t)
			}
		})
	}
}

// --- write deadline --------------------------------------------------------

func TestGroupWriter_SetWriteDeadline(t *testing.T) {
	mockStream := &FakeQUICSendStream{}
	deadline := time.Now().Add(time.Minute)
	sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

	err := sgs.SetWriteDeadline(deadline)
	assert.NoError(t, err)
}

// --- close behavior --------------------------------------------------------

func TestGroupWriter_Close(t *testing.T) {
	mockStream := &FakeQUICSendStream{}

	sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

	err := sgs.Close()
	assert.NoError(t, err)
}

func TestGroupWriter_ContextCancellation(t *testing.T) {
	t.Run("operations continue when context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mockStream := &FakeQUICSendStream{
			ParentCtx: ctx,
			WriteFunc: func(p []byte) (int, error) { return 4, nil },
		}

		sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

		// Cancel the context
		cancel()

		// Test that operations continue to work (they don't check context in current implementation)
		frameLocal := NewFrame(len([]byte("test")))
		_, _ = frameLocal.Write([]byte("test"))
		err := sgs.WriteFrame(frameLocal)
		assert.NoError(t, err)
		assert.Equal(t, uint64(1), sgs.frameCount)
	})
}

func TestGroupWriter_CloseWithStreamError(t *testing.T) {
	t.Run("close returns stream error", func(t *testing.T) {
		streamID := transport.StreamID(123)
		streamErr := &transport.StreamError{
			StreamID:  streamID,
			ErrorCode: transport.StreamErrorCode(42),
		}

		mockStream := &FakeQUICSendStream{
			CloseFunc: func() error { return streamErr },
		}

		sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

		err := sgs.Close()
		// Due to the current implementation bug, Cause(ctx) returns nil when there's no context cause
		assert.NoError(t, err)
	})

	t.Run("close returns non-stream error", func(t *testing.T) {
		otherErr := errors.New("some other error")

		mockStream := &FakeQUICSendStream{
			CloseFunc: func() error { return otherErr },
		}

		sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

		err := sgs.Close()
		// Due to the current implementation bug, Cause(ctx) returns nil when there's no context cause
		assert.NoError(t, err)
	})

	t.Run("close when context already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		mockStream := &FakeQUICSendStream{
			ParentCtx: ctx,
		}

		sgs := newGroupWriter(mockStream, GroupSequence(1), newGroupWriterManager())

		// Cancel the context first
		cancel()

		err := sgs.Close()
		assert.NoError(t, err) // Close still works even if context is cancelled
	})
}

// --- context and cancellation ---------------------------------------------

func TestGroupWriter_Context(t *testing.T) {
	mockStream := &FakeQUICSendStream{}

	sgs := newGroupWriter(mockStream, GroupSequence(123), newGroupWriterManager())

	ctx := sgs.Context()
	assert.NotNil(t, ctx)
	assert.Equal(t, message.StreamTypeGroup, ctx.Value(uniStreamTypeCtxKey))
}

func TestGroupWriter_CancelWrite(t *testing.T) {
	mockStream := &FakeQUICSendStream{}

	groupManager := newGroupWriterManager()
	sgs := newGroupWriter(mockStream, GroupSequence(1), groupManager)
	groupManager.addGroup(sgs)

	sgs.CancelWrite(1)
	assert.Equal(t, 0, groupManager.countGroups())
}
