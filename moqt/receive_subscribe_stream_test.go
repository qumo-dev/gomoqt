package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReceiveSubscribeStream no longer starts any background goroutine: the
// subscribe stream is read only on demand by readUpdate, from the caller's own
// goroutine. So these tests need no goroutine-reaping (no synctest / sleeps)
// except where they explicitly drive readUpdate concurrently.

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
// publisher opts into updates by calling readUpdate.
func TestReceiveSubscribeStream_ConstructorReadsNothing(t *testing.T) {
	// A background reader is proven absent indirectly: queue exactly one real
	// SUBSCRIBE_UPDATE followed by EOF, wait past any window a rogue reader
	// would have consumed it in, then confirm readUpdate() still observes the
	// untouched message. The EOF terminator is load-bearing — without it the
	// queue repeats its last entry, a consumed update would simply be
	// re-served, and the test could never fail.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 9}.Encode(buf))
	mockStream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	t.Cleanup(func() { _ = rss.closeWithError(SubscribeErrorCodeInternal) })

	// A background reader, if one existed, would have consumed the update by now.
	time.Sleep(20 * time.Millisecond)

	got, err := rss.readUpdate()
	require.NoError(t, err, "constructor must not have already consumed the subscribe stream")
	assert.Equal(t, TrackPriority(9), got.Priority)
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

func TestReceiveSubscribeStream_ReadUpdate(t *testing.T) {
	// One SUBSCRIBE_UPDATE waiting on the stream: readUpdate returns its config
	// and makes it the current TrackConfig.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 5}.Encode(buf))
	mockStream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}

	rss := newReceiveSubscribeStream(SubscribeID(123), mockStream, &SubscribeConfig{Priority: TrackPriority(1)})

	got, err := rss.readUpdate()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(5), got.Priority, "returned config carries the update")
	assert.Equal(t, TrackPriority(5), rss.TrackConfig().Priority, "TrackConfig reflects the update")
}

func TestReceiveSubscribeStream_ReadUpdate_Sequence(t *testing.T) {
	// Successive calls return successive updates in order.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 1}.Encode(buf))
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 2}.Encode(buf))
	mockStream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

	first, err := rss.readUpdate()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(1), first.Priority)

	second, err := rss.readUpdate()
	require.NoError(t, err)
	assert.Equal(t, TrackPriority(2), second.Priority)
}

func TestReceiveSubscribeStream_ReadUpdate_ErrorOnStreamEnd(t *testing.T) {
	// A zero-value FakeQUICStream returns io.EOF on Read; readUpdate must surface
	// an error so a caller's read loop terminates.
	rss := newReceiveSubscribeStream(SubscribeID(1), &FakeQUICStream{}, &SubscribeConfig{})

	got, err := rss.readUpdate()
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
			mockStream := &FakeQUICStream{}

			rss := newReceiveSubscribeStream(SubscribeID(123), mockStream, &SubscribeConfig{})

			assert.NoError(t, rss.closeWithError(code))
			cancelled := len(mockStream.CancelReadCodes()) > 0 || len(mockStream.CancelWriteCodes()) > 0
			assert.True(t, cancelled, "closeWithError must cancel the stream")
		})
	}
}

func TestReceiveSubscribeStream_CloseWithError_Idempotent(t *testing.T) {
	// Double close must be safe (no panic, no error) — the caller and the
	// session-teardown path may both close a subscription.
	rss := newReceiveSubscribeStream(SubscribeID(1), &FakeQUICStream{}, &SubscribeConfig{})

	assert.NoError(t, rss.closeWithError(SubscribeErrorCodeInternal))
	assert.NoError(t, rss.closeWithError(SubscribeErrorCodeInternal))
}

func TestReceiveSubscribeStream_ReadUpdate_ConcurrentIsSerialized(t *testing.T) {
	// Two concurrent readUpdate calls must not interleave their Decodes on the
	// stream (readMu serializes them). Two updates are queued; each caller gets
	// exactly one, and both priorities are observed with no torn read.
	buf := &bytes.Buffer{}
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 1}.Encode(buf))
	require.NoError(t, message.SubscribeUpdateMessage{SubscriberPriority: 2}.Encode(buf))
	// FakeQUICStream.Read is internally mutex-serialized, so no extra locking
	// is needed here to make concurrent Read calls safe.
	mockStream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}

	rss := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

	var wg sync.WaitGroup
	got := make([]uint8, 2)
	for i := range 2 {
		wg.Go(func() {
			cfg, err := rss.readUpdate()
			if assert.NoError(t, err) {
				got[i] = uint8(cfg.Priority)
			}
		})
	}
	wg.Wait()

	assert.ElementsMatch(t, []uint8{1, 2}, got, "each caller gets a distinct, intact update")
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
