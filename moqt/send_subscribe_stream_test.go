package moqt

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewSendSubscribeStream(t *testing.T) {
	id := SubscribeID(123)
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: (&bytes.Buffer{}).Read, // Empty buffer returns EOF immediately
	}

	info := PublishInfo{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	assert.NotNil(t, sss, "newSendSubscribeStream should not return nil")
	assert.Equal(t, id, sss.id, "id should be set correctly")
	assert.Equal(t, config, sss.config, "config should be set correctly")
	assert.Equal(t, mockStream, sss.stream, "stream should be set correctly")
}

func TestSendSubscribeStream_SubscribeID(t *testing.T) {
	id := SubscribeID(456)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	info := PublishInfo{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	returnedID := sss.SubscribeID()

	assert.Equal(t, id, returnedID, "SubscribeID() should return the correct ID")
}

func TestSendSubscribeStream_ReadInfo(t *testing.T) {
	id := SubscribeID(999)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	info := PublishInfo{}
	sss := newSendSubscribeStream(id, mockStream, config, info)

	ret := sss.ReadInfo()
	assert.Equal(t, info, ret, "ReadInfo() should return the Info passed to constructor")
}

func TestSendSubscribeStream_TrackConfig(t *testing.T) {
	id := SubscribeID(789)
	config := &SubscribeConfig{
		Priority: TrackPriority(5),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	returnedConfig := sss.TrackConfig()
	assert.Equal(t, config, returnedConfig, "TrackConfig() should return the original config")
	assert.Equal(t, config.Priority, returnedConfig.Priority, "TrackPriority should match")
}

func TestSendSubscribeStream_UpdateSubscribe(t *testing.T) {
	id := SubscribeID(101)
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	// Test valid update
	newConfig := &SubscribeConfig{
		Priority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.NoError(t, err, "updateSubscribe() should not return error for valid config")

	// Verify config was updated
	updatedConfig := sss.TrackConfig()
	assert.Equal(t, newConfig.Priority, updatedConfig.Priority, "TrackPriority should be updated")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribe_InvalidRange(t *testing.T) {
	id := SubscribeID(102)
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	tests := map[string]struct {
		newConfig *SubscribeConfig
		wantError bool
	}{
		"nil config": {
			newConfig: nil,
			wantError: false,
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

func TestSendSubscribeStream_UpdateSubscribe_EncodesWireFormat(t *testing.T) {
	id := SubscribeID(1011)
	config := &SubscribeConfig{Priority: TrackPriority(1)}
	var buf bytes.Buffer
	mockStream := &MockQUICStream{
		WriteFunc: buf.Write,
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	newConfig := &SubscribeConfig{
		Priority:   TrackPriority(7),
		Ordered:    true,
		MaxLatency: 42,
		StartGroup: GroupSequence(10),
		EndGroup:   GroupSequence(20),
	}

	err := sss.updateSubscribe(newConfig)
	require.NoError(t, err)

	var decoded message.SubscribeUpdateMessage
	require.NoError(t, decoded.Decode(&buf))
	assert.Equal(t, uint8(7), decoded.SubscriberPriority)
	assert.Equal(t, uint8(1), decoded.SubscriberOrdered)
	assert.Equal(t, uint64(42), decoded.SubscriberMaxLatency)
	assert.Equal(t, uint64(11), decoded.StartGroup)
	assert.Equal(t, uint64(21), decoded.EndGroup)
	assert.Equal(t, newConfig, sss.TrackConfig())
}

func TestSendSubscribeStream_Close(t *testing.T) {
	id := SubscribeID(103)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Close").Return(nil)
	mockStream.On("CancelRead", mock.Anything).Return()

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	err := sss.close()
	assert.NoError(t, err, "Close() should not return error")

	// Verify Close was called on the underlying stream
	mockStream.AssertCalled(t, "Close")
}

func TestSendSubscribeStream_CloseWithError(t *testing.T) {
	id := SubscribeID(104)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("CancelWrite", StreamErrorCode(SubscribeErrorCodeInternal)).Return()
	mockStream.On("CancelRead", StreamErrorCode(SubscribeErrorCodeInternal)).Return()

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	testErrCode := SubscribeErrorCodeInternal
	sss.closeWithError(testErrCode)

	// Verify CancelWrite and CancelRead were called on the underlying stream
	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(testErrCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(testErrCode))
}

func TestSendSubscribeStream_CloseWithError_NilError(t *testing.T) {
	id := SubscribeID(105)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("CancelWrite", StreamErrorCode(0)).Return()
	mockStream.On("CancelRead", StreamErrorCode(0)).Return()

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	testErrCode := SubscribeErrorCode(0) // Using zero error code
	sss.closeWithError(testErrCode)

	// Should still cancel the stream operations
	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(testErrCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(testErrCode))
}

func TestSendSubscribeStream_ConcurrentUpdate(t *testing.T) {
	id := SubscribeID(106)
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	// Test concurrent updates
	var wg sync.WaitGroup
	wg.Go(func() {
		newConfig := &SubscribeConfig{
			Priority: TrackPriority(2),
		}
		err := sss.updateSubscribe(newConfig)
		if err != nil {
			t.Logf("First concurrent update failed: %v", err)
		}
	})
	wg.Go(func() {
		newConfig := &SubscribeConfig{
			Priority: TrackPriority(3),
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
		finalConfig.Priority, "Final config should be from one of the updates")
}

func TestSendSubscribeStream_ContextCancellation(t *testing.T) {
	t.Skip("sendSubscribeStream no longer exposes internal context")
}

func TestSendSubscribeStream_UpdateSubscribeWriteError(t *testing.T) {
	id := SubscribeID(108)
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	// Mock Write to return an error
	mockStream.On("Write", mock.Anything).Return(0, assert.AnError)
	mockStream.On("CancelWrite", StreamErrorCode(SubscribeErrorCodeInternal)).Return()
	mockStream.On("CancelRead", StreamErrorCode(SubscribeErrorCodeInternal)).Return()

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	newConfig := &SubscribeConfig{
		Priority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.Error(t, err, "updateSubscribe() should return error when Write fails")

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribeClosedStream(t *testing.T) {
	id := SubscribeID(109)
	config := &SubscribeConfig{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}

	mockStream.On("Write", mock.Anything).Return(0, io.EOF)
	mockStream.On("CancelWrite", StreamErrorCode(SubscribeErrorCodeInternal)).Return()
	mockStream.On("CancelRead", StreamErrorCode(SubscribeErrorCodeInternal)).Return()
	mockStream.On("Close").Return(nil)

	sss := newSendSubscribeStream(id, mockStream, config, PublishInfo{})

	// Close the stream first
	err := sss.close()
	assert.NoError(t, err, "Close() should succeed")

	// Try to update after closing
	newConfig := &SubscribeConfig{
		Priority: TrackPriority(1),
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
	mockStream.On("Close").Return(nil).Twice()

	sss := newSendSubscribeStream(SubscribeID(110), mockStream, &SubscribeConfig{}, PublishInfo{})

	// Close once
	err1 := sss.close()
	assert.NoError(t, err1, "first Close() should succeed")

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
	var callCount int
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		callCount++
	}).Return().Twice() // Called twice
	mockStream.On("CancelRead", mock.Anything).Return().Twice() // Called twice

	sss := newSendSubscribeStream(SubscribeID(111), mockStream, &SubscribeConfig{}, PublishInfo{})

	// Close with error once
	testErrCode := SubscribeErrorCodeInternal
	sss.closeWithError(testErrCode)

	// Close with error again - should not fail since the implementation allows multiple calls
	sss.closeWithError(testErrCode)

	mockStream.AssertExpectations(t)
}

func TestSendSubscribeStream_UpdateSubscribeValidRangeTransitions(t *testing.T) {
	id := SubscribeID(112)

	tests := map[string]struct {
		initialConfig *SubscribeConfig
		newConfig     *SubscribeConfig
		expectError   bool
		description   string
	}{
		"update priority": {
			initialConfig: &SubscribeConfig{
				Priority: TrackPriority(1),
			},
			newConfig: &SubscribeConfig{
				Priority: TrackPriority(2),
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

			if !tt.expectError {
				mockStream.On("Write", mock.Anything).Return(0, nil)
			}

			sss := newSendSubscribeStream(id, mockStream, tt.initialConfig, PublishInfo{})

			err := sss.updateSubscribe(tt.newConfig)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				// Verify config was updated
				updatedConfig := sss.TrackConfig()
				assert.Equal(t, tt.newConfig.Priority, updatedConfig.Priority, "config should be updated")
			}

			mockStream.AssertExpectations(t)
		})
	}
}
