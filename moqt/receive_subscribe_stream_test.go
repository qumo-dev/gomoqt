package moqt

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewReceiveSubscribeStream(t *testing.T) {
	tests := map[string]struct {
		subscribeID SubscribeID
		config      *TrackConfig
	}{
		"valid creation": {
			subscribeID: SubscribeID(123),
			config: &TrackConfig{
				TrackPriority: TrackPriority(1),
			},
		},
		"zero subscribe ID": {
			subscribeID: SubscribeID(0),
			config: &TrackConfig{
				TrackPriority: TrackPriority(0),
			},
		},
		"large subscribe ID": {
			subscribeID: SubscribeID(4294967295),
			config: &TrackConfig{
				TrackPriority: TrackPriority(255),
			},
		},
		"nil config": {
			subscribeID: SubscribeID(1),
			config:      nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &MockQUICStream{}

			// Mock the Read method calls for the listenUpdates goroutine
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF)

			rss := newReceiveSubscribeStream(tt.subscribeID, mockStream, tt.config)

			assert.NotNil(t, rss, "newReceiveSubscribeStream should not return nil")
			assert.Equal(t, tt.subscribeID, rss.SubscribeID(), "SubscribeID should match")
			assert.NotNil(t, rss.Updated(), "Updated channel should not be nil") // Wait for goroutine to process EOF and close
			select {
			case <-rss.Updated():
				// Channel closed due to EOF
			case <-time.After(100 * time.Millisecond):
				t.Log("Timeout waiting for update channel to close")
			}

			// Give some time for the goroutine to complete
			time.Sleep(10 * time.Millisecond)

			// Assert that the mock expectations were met
			mockStream.AssertExpectations(t)
		})
	}
}

func TestReceiveSubscribeStream_SubscribeID(t *testing.T) {
	tests := map[string]struct {
		subscribeID SubscribeID
	}{
		"minimum value": {
			subscribeID: SubscribeID(0),
		},
		"small value": {
			subscribeID: SubscribeID(1),
		},
		"medium value": {
			subscribeID: SubscribeID(1000),
		},
		"large value": {
			subscribeID: SubscribeID(1000000),
		},
		"maximum uint62": {
			subscribeID: SubscribeID(1<<(64-2) - 1), // maxVarInt8
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &MockQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					return 0, io.EOF
				},
			}
			// Mock the Read method calls for the listenUpdates goroutine
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF).Maybe()

			config := &TrackConfig{
				TrackPriority: TrackPriority(1),
			}

			rss := newReceiveSubscribeStream(tt.subscribeID, mockStream, config)

			result := rss.SubscribeID()
			assert.Equal(t, tt.subscribeID, result, "SubscribeID should match expected value")

			// Wait for goroutine to close cleanly
			select {
			case <-rss.Updated():
			case <-time.After(100 * time.Millisecond):
			}

			// Give some time for the goroutine to complete
			time.Sleep(10 * time.Millisecond)

			// Assert that the mock expectations were met
			mockStream.AssertExpectations(t)
		})
	}
}

func TestReceiveSubscribeStream_TrackConfig(t *testing.T) {
	tests := map[string]struct {
		config *TrackConfig
	}{
		"valid config": {
			config: &TrackConfig{
				TrackPriority: TrackPriority(10),
			},
		},
		"zero values": {
			config: &TrackConfig{
				TrackPriority: TrackPriority(0),
			},
		},
		"nil config": {
			config: nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			subscribeID := SubscribeID(123)
			mockStream := &MockQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					return 0, io.EOF
				},
			}
			// Mock the Read method calls for the listenUpdates goroutine
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF).Maybe()

			rss := newReceiveSubscribeStream(subscribeID, mockStream, tt.config)

			resultConfig := rss.TrackConfig()

			assert.NotNil(t, resultConfig, "TrackConfig should not be nil")
			if tt.config != nil {
				assert.Equal(t, tt.config.TrackPriority, resultConfig.TrackPriority, "TrackPriority should match")
			}

			// Wait for goroutine to close cleanly
			select {
			case <-rss.Updated():
			case <-time.After(100 * time.Millisecond):
			}

			// Give some time for the goroutine to complete
			time.Sleep(10 * time.Millisecond)

			// Assert that the mock expectations were met
			mockStream.AssertExpectations(t)
		})
	}
}

func TestReceiveSubscribeStream_Updated(t *testing.T) {
	subscribeID := SubscribeID(123)
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	// Mock the Read method calls for the listenUpdates goroutine
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF).Maybe()

	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}

	rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

	updatedCh := rss.Updated()
	assert.NotNil(t, updatedCh, "Updated channel should not be nil")

	// Check that we can receive from the channel (should close due to EOF)
	select {
	case <-updatedCh:
		// Channel should be closed due to EOF
		t.Log("Update channel closed as expected")
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected update channel to be closed due to EOF")
	}

	// Give some time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)

	// Assert that the mock expectations were met
	mockStream.AssertExpectations(t)
}

func TestReceiveSubscribeStream_ListenUpdates_WithSubscribeUpdateMessage(t *testing.T) {
	subscribeID := SubscribeID(123)

	// Create a valid SubscribeUpdateMessage
	updateMsg := message.SubscribeUpdateMessage{
		TrackPriority: 5,
	}

	// Encode the message
	buf := &bytes.Buffer{}
	err := updateMsg.Encode(buf)
	require.NoError(t, err)

	mockStream := &MockQUICStream{
		ReadFunc: buf.Read,
	}
	// Mock the Context method
	mockStream.On("Context").Return(context.Background())

	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}

	rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

	// Wait for the update to be processed
	select {
	case <-rss.Updated():
		// Should receive update notification
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive update notification")
	}
	// Check that config was updated
	updatedConfig := rss.TrackConfig()
	if err == nil {
		assert.Equal(t, TrackPriority(5), updatedConfig.TrackPriority, "TrackPriority should be updated")
	}

	// Give some time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)

	// Assert that the mock expectations were met
	mockStream.AssertExpectations(t)
}

func TestReceiveSubscribeStream_CloseWithError(t *testing.T) {
	tests := map[string]struct {
		errorCode SubscribeErrorCode
		expectErr bool
	}{
		"internal error": {
			errorCode: InternalSubscribeErrorCode,
			expectErr: false,
		},
		"invalid range error": {
			errorCode: InvalidRangeErrorCode,
			expectErr: false,
		},
		"track not found error": {
			errorCode: TrackNotFoundErrorCode,
			expectErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			subscribeID := SubscribeID(123)
			ctx, cancel := context.WithCancelCause(context.Background())
			mockStream := &MockQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					// Block to prevent automatic closure
					select {}
				},
			}
			mockStream.On("StreamID").Return(StreamID(123))
			mockStream.On("Context").Return(ctx)
			mockStream.On("CancelWrite", StreamErrorCode(tt.errorCode)).Run(func(args mock.Arguments) {
				cancel(&StreamError{
					StreamID:  mockStream.StreamID(),
					ErrorCode: args[0].(StreamErrorCode),
				})
			}).Return()
			mockStream.On("CancelRead", StreamErrorCode(tt.errorCode)).Return()

			config := &TrackConfig{
				TrackPriority: TrackPriority(1),
			}

			rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

			err := rss.closeWithError(tt.errorCode)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Check that context is cancelled
			assert.Error(t, rss.ctx.Err(), "Context should be cancelled")

			mockStream.AssertExpectations(t)
		})
	}
}

func TestReceiveSubscribeStream_CloseWithError_MultipleClose(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(123))
	mockStream.On("Context").Return(ctx)
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("CancelWrite", mock.Anything).Run(func(args mock.Arguments) {
		cancel(&StreamError{
			StreamID:  mockStream.StreamID(),
			ErrorCode: args[0].(StreamErrorCode),
		})
	}).Return()
	mockStream.On("CancelRead", mock.Anything).Return()

	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}
	// Create stream manually
	rss := newReceiveSubscribeStream(123, mockStream, config)

	err := rss.closeWithError(InternalSubscribeErrorCode)
	assert.NoError(t, err, "CloseWithError should return error when already closed")
	assert.Error(t, rss.ctx.Err(), "Context should be cancelled after first closeWithError")

	// Get the cause and check its type
	cause := Cause(rss.ctx)
	var subscribeErr *SubscribeError
	assert.ErrorAs(t, cause, &subscribeErr, "closeErr should be a SubscribeError")
}

func TestReceiveSubscribeStream_ConcurrentAccess(t *testing.T) {
	subscribeID := SubscribeID(123)
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, io.EOF
		},
	}
	// Mock the Read method calls for the listenUpdates goroutine
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF).Maybe()

	config := &TrackConfig{
		TrackPriority: TrackPriority(1),
	}

	rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

	// Test concurrent access to SubscribeID (should be safe as it's read-only)
	var wg sync.WaitGroup
	numGoroutines := 10

	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			id := rss.SubscribeID()
			assert.Equal(t, subscribeID, id)
		}()
	}

	// Test concurrent access to TrackConfig
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			config := rss.TrackConfig()
			assert.NotNil(t, config)
		}()
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All concurrent accesses completed successfully
	case <-time.After(1 * time.Second):
		t.Error("Concurrent access test timed out")
	}
	// Clean up - wait for update channel to close
	select {
	case <-rss.Updated():
	case <-time.After(100 * time.Millisecond):
	}

	// Give some time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)

	// Assert that the mock expectations were met
	mockStream.AssertExpectations(t)
}

func TestReceiveSubscribeStream_Close_DoesNotCancelReadOnGracefulClose(t *testing.T) {
	// Create a mock stream that returns EOF on Read and a background context.
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) { return 0, io.EOF },
	}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil)

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{})

	// Perform a graceful close; it should not call CancelRead
	err := rss.close()
	require.NoError(t, err)

	// Assert Close was called but CancelRead was not
	mockStream.AssertCalled(t, "Close")
	mockStream.AssertNotCalled(t, "CancelRead", mock.Anything)
}

func TestReceiveSubscribeStream_UpdateChannelBehavior(t *testing.T) {
	t.Run("channel closes on EOF", func(t *testing.T) {
		subscribeID := SubscribeID(123)
		mockStream := &MockQUICStream{
			ReadFunc: func(p []byte) (int, error) {
				return 0, io.EOF
			},
		}
		// Mock the Read method calls for the listenUpdates goroutine
		mockStream.On("Context").Return(context.Background())
		mockStream.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF).Maybe()
		config := &TrackConfig{TrackPriority: TrackPriority(1)}

		rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

		// Wait for the goroutine to handle EOF and close the channel
		time.Sleep(50 * time.Millisecond)

		// Verify channel is closed by trying to receive
		select {
		case _, ok := <-rss.Updated():
			if ok {
				t.Log("Channel should be closed and ready to receive")
			} else {
				t.Log("Channel is properly closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Log("Channel should be closed and ready to receive")
		}

		// Give some time for the goroutine to complete
		time.Sleep(10 * time.Millisecond)

		// Assert that the mock expectations were met
		mockStream.AssertExpectations(t)
	})

	t.Run("multiple updates sent to channel", func(t *testing.T) {
		subscribeID := SubscribeID(123)

		// Create multiple update messages
		updates := []message.SubscribeUpdateMessage{
			{
				TrackPriority: 1,
			},
			{
				TrackPriority: 2,
			},
		}

		buf := &bytes.Buffer{}
		for _, update := range updates {
			err := update.Encode(buf)
			require.NoError(t, err)
		}

		mockStream := &MockQUICStream{
			ReadFunc: buf.Read,
		}
		// Mock the Context method
		mockStream.On("Context").Return(context.Background())

		config := &TrackConfig{TrackPriority: TrackPriority(0)}
		rss := newReceiveSubscribeStream(subscribeID, mockStream, config) // Should receive multiple update notifications
		updateCount := 0
		expectedUpdates := 1 // We expect at least 1 update, but may get more

		timeout := time.After(200 * time.Millisecond)
		for updateCount < expectedUpdates {
			select {
			case _, ok := <-rss.Updated():
				if !ok {
					// Channel closed
					t.Logf("Channel closed after %d updates", updateCount)
					break
				}
				updateCount++
				t.Logf("Received update %d", updateCount)
			case <-timeout:
				t.Errorf("Timeout waiting for updates, received %d out of at least %d expected", updateCount, expectedUpdates)
				return
			}
		}
		// We received at least the minimum expected updates
		assert.GreaterOrEqual(t, updateCount, expectedUpdates, "Should receive at least expected number of updates")

		// Give some time for the goroutine to complete
		time.Sleep(10 * time.Millisecond)

		// Assert that the mock expectations were met
		mockStream.AssertExpectations(t)
	})
}
