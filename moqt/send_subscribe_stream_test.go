package moqt

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewSendSubscribeStream(t *testing.T) {
	id := SubscribeID(123)
	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: (&bytes.Buffer{}).Read, // Empty buffer returns EOF immediately
	}
	mockStream.On("Context").Return(context.Background())

	info := Info{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	assert.NotNil(t, sss, "newSendSubscribeStream should not return nil")
	assert.Equal(t, id, sss.id, "id should be set correctly")
	assert.Equal(t, config, sss.config, "config should be set correctly")
	assert.Equal(t, mockStream, sss.stream, "stream should be set correctly")
	assert.False(t, sss.ctx.Err() != nil, "stream should not be closed initially")
}

func TestSendSubscribeStream_SubscribeID(t *testing.T) {
	id := SubscribeID(456)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Context").Return(context.Background())

	info := Info{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	returnedID := sss.SubscribeID()

	assert.Equal(t, id, returnedID, "SubscribeID() should return the correct ID")
}

func TestSendSubscribeStream_ReadInfo(t *testing.T) {
	id := SubscribeID(999)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Context").Return(context.Background())

	info := Info{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	ret := sss.ReadInfo()
	assert.Equal(t, info, ret, "ReadInfo() should return the Info passed to constructor")
}

func TestSendSubscribeStream_TrackConfig(t *testing.T) {
	id := SubscribeID(789)
	config := &TrackConfig{
		TrackPriority: TrackPriority(5),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Context").Return(context.Background())

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	returnedConfig := sss.TrackConfig()
	assert.Equal(t, config, returnedConfig, "TrackConfig() should return the original config")
	assert.Equal(t, config.TrackPriority, returnedConfig.TrackPriority, "TrackPriority should match")
}

func TestSendSubscribeStream_UpdateSubscribe(t *testing.T) {
	id := SubscribeID(101)
	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	// Test valid update
	newConfig := &TrackConfig{
		TrackPriority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.NoError(t, err, "updateSubscribe() should not return error for valid config")

	// Verify config was updated
	updatedConfig := sss.TrackConfig()
	assert.Equal(t, newConfig.TrackPriority, updatedConfig.TrackPriority, "TrackPriority should be updated")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribe_InvalidRange(t *testing.T) {
	id := SubscribeID(102)
	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Context").Return(context.Background())

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	tests := map[string]struct {
		newConfig *TrackConfig
		wantError bool
	}{
		"nil config": {
			newConfig: nil,
			wantError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := sss.updateSubscribe(tt.newConfig)
			if tt.wantError {
				assert.Error(t, err, "updateSubscribe() should return error for %s", name)
			} else {
				assert.NoError(t, err, "updateSubscribe() should not return error for %s", name)
			}
		})
	}
}

func TestSendSubscribeStream_Close(t *testing.T) {
	id := SubscribeID(103)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)
	mockStream.On("Close").Run(func(args mock.Arguments) {
		cancel(nil) // Simulate stream closure cancelling the context
	}).Return(nil)
	mockStream.On("CancelRead", mock.Anything).Return()

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	err := sss.close()
	assert.NoError(t, err, "Close() should not return error")
	assert.True(t, sss.ctx.Err() != nil, "stream should be marked as closed")

	// Verify Close was called on the underlying stream
	mockStream.AssertCalled(t, "Close")
}

func TestSendSubscribeStream_CloseWithError(t *testing.T) {
	id := SubscribeID(104)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	mockStream.On("StreamID").Return(StreamID(1))
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		cancel(&StreamError{
			StreamID:  StreamID(1),
			ErrorCode: StreamErrorCode(args[0].(StreamErrorCode)),
		})
	}).Return()
	mockStream.On("CancelRead", mock.Anything).Return()

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	testErrCode := InternalSubscribeErrorCode
	err := sss.closeWithError(testErrCode)
	assert.NoError(t, err, "CloseWithError() should not return error")
	assert.True(t, sss.ctx.Err() != nil, "stream should be marked as closed")

	// Check the stored error directly
	var subscribeErr *SubscribeError
	assert.ErrorAs(t, Cause(sss.ctx), &subscribeErr, "closeErr should be a SubscribeError")

	// Verify CancelWrite and CancelRead were called on the underlying stream
	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(testErrCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(testErrCode))
}

func TestSendSubscribeStream_CloseWithError_NilError(t *testing.T) {
	id := SubscribeID(105)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	mockStream.On("StreamID").Return(StreamID(1))
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		cancel(&StreamError{
			StreamID:  StreamID(1),
			ErrorCode: StreamErrorCode(args[0].(StreamErrorCode)), // 型はStreamErrorCodeで渡される
		})
	}).Return()
	mockStream.On("CancelRead", mock.Anything).Return()

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	testErrCode := SubscribeErrorCode(0) // Using zero error code
	err := sss.closeWithError(testErrCode)
	assert.NoError(t, err, "CloseWithError() should not return error")
	assert.True(t, sss.ctx.Err() != nil, "stream should be marked as closed")

	// Should still cancel the stream operations
	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(testErrCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(testErrCode))
}

func TestSendSubscribeStream_ConcurrentUpdate(t *testing.T) {
	id := SubscribeID(106)
	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	mockStream.On("Context").Return(context.Background())
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	// Test concurrent updates
	var wg sync.WaitGroup
	wg.Go(func() {
		newConfig := &TrackConfig{
			TrackPriority: TrackPriority(2),
		}
		err := sss.updateSubscribe(newConfig)
		if err != nil {
			t.Logf("First concurrent update failed: %v", err)
		}
	})
	wg.Go(func() {
		newConfig := &TrackConfig{
			TrackPriority: TrackPriority(3),
		}
		err := sss.updateSubscribe(newConfig)
		if err != nil {
			t.Logf("Second concurrent update failed: %v", err)
		}
	})

	// Wait for both goroutines to complete
	wg.Wait()

	// Both updates should have completed without crashing
	// The final config should be one of the two updates
	finalConfig := sss.TrackConfig()
	assert.Contains(t, []TrackPriority{TrackPriority(2), TrackPriority(3)},
		finalConfig.TrackPriority, "Final config should be from one of the updates")
}

func TestSendSubscribeStream_ContextCancellation(t *testing.T) {
	id := SubscribeID(107)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	mockStream.On("Context").Return(ctx)

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	// Cancel the context
	cancel()

	// Check that the stream's context is properly cancelled
	select {
	case <-sss.ctx.Done():
		// Context should be cancelled
		assert.Error(t, sss.ctx.Err(), "context should have an error when cancelled")
	default:
		t.Error("context should be cancelled")
	}
}

func TestSendSubscribeStream_UpdateSubscribeWriteError(t *testing.T) {
	id := SubscribeID(108)
	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	// Mock Write to return an error
	mockStream.On("Write", mock.Anything).Return(0, assert.AnError)
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		cancel(&StreamError{
			StreamID:  StreamID(1),
			ErrorCode: StreamErrorCode(args[0].(StreamErrorCode)),
		})
	}).Return()
	mockStream.On("CancelRead", mock.Anything).Return()

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	newConfig := &TrackConfig{
		TrackPriority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.Error(t, err, "updateSubscribe() should return error when Write fails")
	assert.Error(t, sss.ctx.Err(), "stream should be marked as closed after write error")

	// Check the stored error directly
	assert.Error(t, Cause(sss.ctx), "closeErr should be set")
	var subscribeErr *SubscribeError
	assert.ErrorAs(t, Cause(sss.ctx), &subscribeErr, "closeErr should be a SubscribeError")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribeClosedStream(t *testing.T) {
	id := SubscribeID(109)
	config := &TrackConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Close").Run(func(args mock.Arguments) {
		cancel(nil) // Simulate stream closure cancelling the context
	}).Return(nil)
	mockStream.On("Context").Return(ctx)

	sss := newSendSubscribeStream(id, mockStream, config, Info{})

	// Close the stream first
	err := sss.close()
	assert.NoError(t, err, "Close() should succeed")

	// Try to update after closing
	newConfig := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}

	err = sss.updateSubscribe(newConfig)
	assert.Error(t, err, "updateSubscribe() should return error on closed stream")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_CloseAlreadyClosed(t *testing.T) {
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)
	mockStream.On("Close").Run(func(args mock.Arguments) {
		cancel(nil)
	}).Return(nil)

	sss := newSendSubscribeStream(SubscribeID(110), mockStream, &TrackConfig{}, Info{})

	// Close once
	err1 := sss.close()
	assert.NoError(t, err1, "first Close() should succeed")
	assert.True(t, sss.ctx.Err() != nil, "stream should be marked as closed")

	// Close again
	err2 := sss.close()
	assert.NoError(t, err2, "second Close() should not return error")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_CloseWithError_MultipleClose(t *testing.T) {
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream.On("Context").Return(ctx)

	var callCount int
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		callCount++
		if callCount == 1 {
			cancel(&StreamError{
				StreamID:  StreamID(1),
				ErrorCode: StreamErrorCode(args[0].(StreamErrorCode)),
			})
		}
	}).Return().Twice() // Called twice
	mockStream.On("CancelRead", mock.Anything).Return().Twice() // Called twice

	sss := newSendSubscribeStream(SubscribeID(111), mockStream, &TrackConfig{}, Info{})

	// Close with error once
	testErrCode := InternalSubscribeErrorCode
	err1 := sss.closeWithError(testErrCode)
	assert.NoError(t, err1, "first CloseWithError() should succeed")
	assert.True(t, sss.ctx.Err() != nil, "stream should be marked as closed")

	// Close with error again - should not fail since the implementation allows multiple calls
	err2 := sss.closeWithError(testErrCode)
	assert.NoError(t, err2, "second CloseWithError() should not return error")

	// Check the stored error directly
	assert.Error(t, Cause(sss.ctx), "closeErr should be set")
	var subscribeErr *SubscribeError
	assert.ErrorAs(t, Cause(sss.ctx), &subscribeErr, "closeErr should be a SubscribeError")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribeValidRangeTransitions(t *testing.T) {
	id := SubscribeID(112)

	tests := map[string]struct {
		initialConfig *TrackConfig
		newConfig     *TrackConfig
		expectError   bool
		description   string
	}{
		"update priority": {
			initialConfig: &TrackConfig{
				TrackPriority: TrackPriority(1),
			},
			newConfig: &TrackConfig{
				TrackPriority: TrackPriority(2),
			},
			expectError: false,
			description: "should allow updating priority",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &MockQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					return 0, io.EOF
				},
			}
			mockStream.On("Context").Return(context.Background())

			if !tt.expectError {
				mockStream.On("Write", mock.Anything).Return(0, nil)
			}

			sss := newSendSubscribeStream(id, mockStream, tt.initialConfig, Info{})

			err := sss.updateSubscribe(tt.newConfig)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				// Verify config was updated
				updatedConfig := sss.TrackConfig()
				assert.Equal(t, tt.newConfig, updatedConfig, "config should be updated")
			}

			mockStream.AssertExpectations(t)
		})
	}
}
