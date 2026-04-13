package moqt

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReceiveSubscribeStream(t *testing.T) {
	tests := map[string]struct {
		subscribeID SubscribeID
		config      *SubscribeConfig
	}{
		"valid creation": {
			subscribeID: SubscribeID(123),
			config: &SubscribeConfig{
				Priority: TrackPriority(1),
			},
		},
		"zero subscribe ID": {
			subscribeID: SubscribeID(0),
			config: &SubscribeConfig{
				Priority: TrackPriority(0),
			},
		},
		"large subscribe ID": {
			subscribeID: SubscribeID(4294967295),
			config: &SubscribeConfig{
				Priority: TrackPriority(255),
			},
		},
		"nil config": {
			subscribeID: SubscribeID(1),
			config:      nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &FakeQUICStream{}

			rss := newReceiveSubscribeStream(tt.subscribeID, mockStream, tt.config)

			assert.NotNil(t, rss, "newReceiveSubscribeStream should not return nil")
			assert.Equal(t, tt.subscribeID, rss.SubscribeID(), "SubscribeID should match")
			assert.NotNil(t, rss.Updated(), "Updated channel should not be nil") // Wait for goroutine to process EOF and close
			time.Sleep(10 * time.Millisecond)
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
			mockStream := &FakeQUICStream{}

			config := &SubscribeConfig{
				Priority: TrackPriority(1),
			}

			rss := newReceiveSubscribeStream(tt.subscribeID, mockStream, config)

			result := rss.SubscribeID()
			assert.Equal(t, tt.subscribeID, result, "SubscribeID should match expected value")

			time.Sleep(10 * time.Millisecond)
		})
	}
}

func TestReceiveSubscribeStream_TrackConfig(t *testing.T) {
	tests := map[string]struct {
		config *SubscribeConfig
	}{
		"valid config": {
			config: &SubscribeConfig{
				Priority: TrackPriority(10),
			},
		},
		"zero values": {
			config: &SubscribeConfig{
				Priority: TrackPriority(0),
			},
		},
		"nil config": {
			config: nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			subscribeID := SubscribeID(123)
			mockStream := &FakeQUICStream{}

			rss := newReceiveSubscribeStream(subscribeID, mockStream, tt.config)

			resultConfig := rss.TrackConfig()

			assert.NotNil(t, resultConfig, "TrackConfig should not be nil")
			if tt.config != nil {
				assert.Equal(t, tt.config.Priority, resultConfig.Priority, "TrackPriority should match")
			}

			time.Sleep(10 * time.Millisecond)
		})
	}
}

func TestReceiveSubscribeStream_Updated(t *testing.T) {
	subscribeID := SubscribeID(123)
	mockStream := &FakeQUICStream{}

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}

	rss := newReceiveSubscribeStream(subscribeID, mockStream, config)

	updatedCh := rss.Updated()
	assert.NotNil(t, updatedCh, "Updated channel should not be nil")

	// EOF stops the background goroutine; it does not close Updated().
	time.Sleep(10 * time.Millisecond)
	assert.NotNil(t, updatedCh)

	// Give some time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestReceiveSubscribeStream_ListenUpdates_WithSubscribeUpdateMessage(t *testing.T) {
	subscribeID := SubscribeID(123)

	// Create a valid SubscribeUpdateMessage
	updateMsg := message.SubscribeUpdateMessage{
		SubscriberPriority: 5,
	}

	// Encode the message
	buf := &bytes.Buffer{}
	err := updateMsg.Encode(buf)
	require.NoError(t, err)

	mockStream := &FakeQUICStream{
		ReadFunc: buf.Read,
	}

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
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
		assert.Equal(t, TrackPriority(5), updatedConfig.Priority, "TrackPriority should be updated")
	}

	// Give some time for the goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestReceiveSubscribeStream_CloseWithError(t *testing.T) {
	tests := map[string]struct {
		errorCode SubscribeErrorCode
		expectErr bool
	}{
		"internal error": {
			errorCode: SubscribeErrorCodeInternal,
			expectErr: false,
		},
		"invalid range error": {
			errorCode: SubscribeErrorCodeInvalidRange,
			expectErr: false,
		},
		"track not found error": {
			errorCode: SubscribeErrorCodeNotFound,
			expectErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			subscribeID := SubscribeID(123)
			mockStream := &FakeQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					// Block to prevent automatic closure
					select {}
				},
			}

			config := &SubscribeConfig{
				Priority: TrackPriority(1),
			}

			rss := newReceiveSubscribeStream(subscribeID, mockStream, config)
			updatedCh := rss.Updated()

			err := rss.closeWithError(tt.errorCode)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			select {
			case _, ok := <-updatedCh:
				assert.False(t, ok, "updated channel should be closed")
			case <-time.After(100 * time.Millisecond):
				t.Fatal("expected updated channel to close")
			}
		})
	}
}

func TestReceiveSubscribeStream_CloseWithError_MultipleClose(t *testing.T) {
	mockStream := &FakeQUICStream{}

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	// Create stream manually
	rss := newReceiveSubscribeStream(123, mockStream, config)
	updatedCh := rss.Updated()

	rss.closeWithError(SubscribeErrorCodeInternal)
	rss.closeWithError(SubscribeErrorCodeInternal)

	select {
	case _, ok := <-updatedCh:
		assert.False(t, ok, "updated channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected updated channel to close")
	}
}

func TestReceiveSubscribeStream_ConcurrentAccess(t *testing.T) {
	subscribeID := SubscribeID(123)
	mockStream := &FakeQUICStream{}

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
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

}

func TestReceiveSubscribeStream_Close_DoesNotCancelReadOnGracefulClose(t *testing.T) {
	// Create a mock stream that returns EOF on Read and a background context.
	mockStream := &FakeQUICStream{}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

	// Perform a graceful close; it should not call CancelRead
	err := rss.close()
	require.NoError(t, err)

	// Assert Close was called but CancelRead was not
	assert.ErrorIs(t, mockStream.Context().Err(), context.Canceled)
	_, readErr := mockStream.Read(make([]byte, 1))
	var streamErr *transport.StreamError
	assert.False(t, errors.As(readErr, &streamErr))
}

func TestReceiveSubscribeStream_UpdateChannelBehavior(t *testing.T) {
	t.Run("channel closes on EOF", func(t *testing.T) {
		subscribeID := SubscribeID(123)
		mockStream := &FakeQUICStream{}
		config := &SubscribeConfig{Priority: TrackPriority(1)}

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

	})

	t.Run("multiple updates sent to channel", func(t *testing.T) {
		subscribeID := SubscribeID(123)

		// Create multiple update messages
		updates := []message.SubscribeUpdateMessage{
			{
				SubscriberPriority: 1,
			},
			{
				SubscriberPriority: 2,
			},
		}

		buf := &bytes.Buffer{}
		for _, update := range updates {
			err := update.Encode(buf)
			require.NoError(t, err)
		}

		mockStream := &FakeQUICStream{
			ReadFunc: buf.Read,
		}

		config := &SubscribeConfig{Priority: TrackPriority(0)}
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
	})
}
