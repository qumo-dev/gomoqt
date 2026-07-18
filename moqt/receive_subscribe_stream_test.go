package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReceiveSubscribeStream no longer starts any background goroutine: the
// subscribe stream is read only on demand by subscribeUpdated, from the caller's
// own goroutine. So these tests need no goroutine-reaping (no synctest / sleeps)
// except where they explicitly drive subscribeUpdated concurrently.

func TestNewReceiveSubscribeStream(t *testing.T) {
	tests := map[string]struct {
		subscribeID SubscribeID
		config      *SubscribeConfig
	}{
		"valid creation":    {subscribeID: SubscribeID(123), config: &SubscribeConfig{Priority: TrackPriority(1)}},
		"zero subscribe ID": {subscribeID: SubscribeID(0), config: &SubscribeConfig{Priority: TrackPriority(0)}},
		"large subscribe ID": {
			subscribeID: SubscribeID(4294967295),
			config:      &SubscribeConfig{Priority: TrackPriority(255)},
		},
		"nil config": {subscribeID: SubscribeID(1), config: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rss := newReceiveSubscribeStream(tt.subscribeID, &FakeQUICStream{}, tt.config)

			assert.NotNil(t, rss, "newReceiveSubscribeStream should not return nil")
			assert.Equal(t, tt.subscribeID, rss.SubscribeID(), "SubscribeID should match")
		})
	}
}

// TestReceiveSubscribeStream_ConstructorReadsNothing is the core property behind
// the goroutine reduction: constructing a subscription must not read the stream
// (i.e. must not start a background reader). The stream is touched only when the
// publisher opts into updates by calling subscribeUpdated.
func TestReceiveSubscribeStream_ConstructorReadsNothing(t *testing.T) {
	var reads atomic.Int64
	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			reads.Add(1)
			select {} // a real reader would block here
		},
	}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	t.Cleanup(func() { _ = rss.closeWithError(SubscribeErrorCodeInternal) })

	// A background reader, if one existed, would have called Read by now.
	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, reads.Load(), "constructor must not read the subscribe stream")
}

func TestReceiveSubscribeStream_SubscribeID(t *testing.T) {
	tests := map[string]SubscribeID{
		"minimum value":  SubscribeID(0),
		"small value":    SubscribeID(1),
		"medium value":   SubscribeID(1000),
		"large value":    SubscribeID(1000000),
		"maximum uint62": SubscribeID(1<<(64-2) - 1), // maxVarInt8
	}
	for name, id := range tests {
		t.Run(name, func(t *testing.T) {
			rss := newReceiveSubscribeStream(id, &FakeQUICStream{}, &SubscribeConfig{Priority: TrackPriority(1)})
			assert.Equal(t, id, rss.SubscribeID(), "SubscribeID should match expected value")
		})
	}
}

func TestReceiveSubscribeStream_TrackConfig(t *testing.T) {
	tests := map[string]struct {
		config *SubscribeConfig
	}{
		"valid config": {config: &SubscribeConfig{Priority: TrackPriority(10)}},
		"zero values":  {config: &SubscribeConfig{Priority: TrackPriority(0)}},
		"nil config":   {config: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rss := newReceiveSubscribeStream(SubscribeID(123), &FakeQUICStream{}, tt.config)

			resultConfig := rss.TrackConfig()

			assert.NotNil(t, resultConfig, "TrackConfig should not be nil")
			if tt.config != nil {
				assert.Equal(t, tt.config.Priority, resultConfig.Priority, "TrackPriority should match")
			}
		})
	}
}

func TestReceiveSubscribeStream_SubscribeUpdated(t *testing.T) {
	// One SUBSCRIBE_UPDATE waiting on the stream: subscribeUpdated returns its
	// config and makes it the current TrackConfig.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 5}.Encode(buf))
	mockStream := &FakeQUICStream{ReadFunc: buf.Read}

	rss := newReceiveSubscribeStream(SubscribeID(123), mockStream, &SubscribeConfig{Priority: TrackPriority(1)})

	got, err := rss.subscribeUpdated()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(5), got.Priority, "returned config carries the update")
	assert.Equal(t, TrackPriority(5), rss.TrackConfig().Priority, "TrackConfig reflects the update")
}

func TestReceiveSubscribeStream_SubscribeUpdated_Sequence(t *testing.T) {
	// Successive calls return successive updates in order.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 1}.Encode(buf))
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 2}.Encode(buf))
	mockStream := &FakeQUICStream{ReadFunc: buf.Read}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

	first, err := rss.subscribeUpdated()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(1), first.Priority)

	second, err := rss.subscribeUpdated()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(2), second.Priority)
}

func TestReceiveSubscribeStream_SubscribeUpdated_ErrorOnStreamEnd(t *testing.T) {
	// A zero-value FakeQUICStream returns io.EOF on Read; subscribeUpdated must
	// surface an error so a caller's read loop terminates.
	rss := newReceiveSubscribeStream(SubscribeID(1), &FakeQUICStream{}, &SubscribeConfig{})

	got, err := rss.subscribeUpdated()
	assert.Error(t, err, "stream end must return an error")
	assert.Nil(t, got)
}

func TestReceiveSubscribeStream_CloseWithError(t *testing.T) {
	tests := map[string]SubscribeErrorCode{
		"internal error":        SubscribeErrorCodeInternal,
		"invalid range error":   SubscribeErrorCodeInvalidRange,
		"track not found error": SubscribeErrorCodeNotFound,
	}
	for name, code := range tests {
		t.Run(name, func(t *testing.T) {
			var cancelled atomic.Bool
			mockStream := &FakeQUICStream{
				CancelReadFunc:  func(transport.StreamErrorCode) { cancelled.Store(true) },
				CancelWriteFunc: func(transport.StreamErrorCode) { cancelled.Store(true) },
			}

			rss := newReceiveSubscribeStream(SubscribeID(123), mockStream, &SubscribeConfig{})

			assert.NoError(t, rss.closeWithError(code))
			assert.True(t, cancelled.Load(), "closeWithError must cancel the stream")
		})
	}
}

func TestReceiveSubscribeStream_ConcurrentAccess(t *testing.T) {
	rss := newReceiveSubscribeStream(SubscribeID(123), &FakeQUICStream{}, &SubscribeConfig{Priority: TrackPriority(1)})

	var wg sync.WaitGroup
	const numGoroutines = 10

	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			assert.Equal(t, SubscribeID(123), rss.SubscribeID())
		}()
	}

	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			assert.NotNil(t, rss.TrackConfig())
		}()
	}

	wg.Wait()
}

func TestReceiveSubscribeStream_Close_DoesNotCancelReadOnGracefulClose(t *testing.T) {
	mockStream := &FakeQUICStream{}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

	// A graceful close must not call CancelRead.
	require.NoError(t, rss.close())

	assert.ErrorIs(t, mockStream.Context().Err(), context.Canceled)
	_, readErr := mockStream.Read(make([]byte, 1))
	var streamErr *transport.StreamError
	assert.False(t, errors.As(readErr, &streamErr))
	assert.ErrorIs(t, readErr, io.EOF)
}
