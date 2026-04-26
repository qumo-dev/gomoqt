package moqt

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/moqttrace"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// func newTestSendSubscribeStream(tb testing.TB) (*sendSubscribeStream, *FakeQUICStream) {
// 	tb.Helper()
// 	stream := &FakeQUICStream{}
// 	substr := newSendSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{}, nil)
// 	go substr.readSubscribeResponses()
// 	return substr, stream
// }

func newTestSendSubscribeStream(stream transport.Stream) *sendSubscribeStream {
	substr := newSendSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{}, nil)
	go substr.readSubscribeResponses()
	return substr
}

func TestNewSendSubscribeStream(t *testing.T) {
	id := SubscribeID(123)
	config := &SubscribeConfig{
		Priority:   TrackPriority(1),
		Ordered:    true,
		MaxLatency: 1000,
		StartGroup: 3,
		EndGroup:   5,
	}
	mockStream := &FakeQUICStream{}
	trace := &moqttrace.SessionTrace{}

	sss := newSendSubscribeStream(id, mockStream, config, trace)

	assert.NotNil(t, sss, "newSendSubscribeStream should not return nil")
	assert.Equal(t, id, sss.id, "id should be set correctly")
	assert.Equal(t, config, sss.config, "config should be set correctly")
	assert.Equal(t, mockStream, sss.stream, "stream should be set correctly")
	assert.Equal(t, trace, sss.trace, "trace should be set correctly")
}

func TestSendSubscribeStream_SubscribeID(t *testing.T) {
	id := SubscribeID(456)

	substr := newSendSubscribeStream(id, &FakeQUICStream{}, &SubscribeConfig{}, nil)

	returnedID := substr.SubscribeID()

	assert.Equal(t, id, returnedID, "SubscribeID() should return the correct ID")
}

func TestSendSubscribeStream_ReadInfo(t *testing.T) {
	sss := newTestSendSubscribeStream(&FakeQUICStream{})

	ret := sss.ReadInfo()
	assert.Equal(t, PublishInfo{}, ret, "ReadInfo() should return the Info passed to constructor")
}

func TestSendSubscribeStream_TrackConfig(t *testing.T) {
	id := SubscribeID(123)
	config := &SubscribeConfig{
		Priority:   TrackPriority(1),
		Ordered:    true,
		MaxLatency: 1000,
		StartGroup: 3,
		EndGroup:   5,
	}
	mockStream := &FakeQUICStream{}
	trace := &moqttrace.SessionTrace{}

	substr := newSendSubscribeStream(id, mockStream, config, trace)

	returnedConfig := substr.TrackConfig()
	assert.Equal(t, config, returnedConfig, "TrackConfig() should return the original config")
}

func TestSendSubscribeStream_UpdateSubscribe(t *testing.T) {
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, config, nil)

	// Test valid update
	newConfig := &SubscribeConfig{
		Priority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.NoError(t, err, "updateSubscribe() should not return error for valid config")

	// Verify config was updated
	updatedConfig := sss.TrackConfig()
	assert.Equal(t, newConfig.Priority, updatedConfig.Priority, "TrackPriority should be updated")

}

func TestSendSubscribeStream_UpdateSubscribe_NilConfigNoOp(t *testing.T) {
	sss := newTestSendSubscribeStream(&FakeQUICStream{})

	beforeConfig := sss.TrackConfig()

	err := sss.updateSubscribe(nil)
	assert.NoError(t, err, "updateSubscribe() should ignore nil config")
	afterConfig := sss.TrackConfig()
	assert.Equal(t, beforeConfig.Priority, afterConfig.Priority, "TrackPriority should not change")
}

func TestSendSubscribeStream_UpdateSubscribe_EncodesWireFormat(t *testing.T) {
	config := &SubscribeConfig{Priority: TrackPriority(1)}
	var buf bytes.Buffer
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = buf.Write
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, config, nil)

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
	mockStream := &FakeQUICStream{}
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, nil)

	err := sss.close()
	assert.NoError(t, err, "Close() should not return error")

	// Verify Close was called on the underlying stream
	assert.ErrorIs(t, mockStream.Context().Err(), context.Canceled)
}

func TestSendSubscribeStream_CloseWithError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, nil)

	testErrCode := SubscribeErrorCodeInternal
	sss.closeWithError(testErrCode)

	// Verify CancelWrite was called on the underlying stream
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(testErrCode), cancelWriteErr.ErrorCode)

	// Verify CancelRead was called on the underlying stream
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(testErrCode), cancelReadErr.ErrorCode)
}

func TestSendSubscribeStream_CloseWithError_NilError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, nil)

	testErrCode := SubscribeErrorCode(0) // Using zero error code
	sss.closeWithError(testErrCode)

	// Verify CancelWrite was called on the underlying stream
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(testErrCode), cancelWriteErr.ErrorCode)

	// Verify CancelRead was called on the underlying stream
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(testErrCode), cancelReadErr.ErrorCode)
}

func TestSendSubscribeStream_ConcurrentUpdate(t *testing.T) {
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, config, nil)

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
	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, assert.AnError }
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, config, nil)

	newConfig := &SubscribeConfig{
		Priority: TrackPriority(2),
	}

	err := sss.updateSubscribe(newConfig)
	assert.Error(t, err, "updateSubscribe() should return error when Write fails")

}

func TestSendSubscribeStream_UpdateSubscribeClosedStream(t *testing.T) {
	config := &SubscribeConfig{}

	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, io.EOF }
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, config, nil)

	// Close the stream first
	err := sss.close()
	assert.NoError(t, err, "Close() should succeed")

	// Try to update after closing
	newConfig := &SubscribeConfig{
		Priority: TrackPriority(1),
	}

	err = sss.updateSubscribe(newConfig)
	assert.Error(t, err, "updateSubscribe() should return error on closed stream")

}

func TestSendSubscribeStream_CloseAlreadyClosed(t *testing.T) {
	mockStream := &FakeQUICStream{}
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, nil)

	// Close once
	err1 := sss.close()
	assert.NoError(t, err1, "first Close() should succeed")

	// Close again
	err2 := sss.close()
	assert.NoError(t, err2, "second Close() should not return error")
}

func TestSendSubscribeStream_CloseWithError_MultipleClose(t *testing.T) {
	mockStream := &FakeQUICStream{}
	sss := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, nil)

	// Close with error once
	testErrCode := SubscribeErrorCodeInternal
	sss.closeWithError(testErrCode)

	// Close with error again - should not fail since the implementation allows multiple calls
	sss.closeWithError(testErrCode)
}

func TestSendSubscribeStream_UpdateSubscribeValidRangeTransitions(t *testing.T) {

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
			mockStream := &FakeQUICStream{}
			if !tt.expectError {
				mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
			}
			sss := newSendSubscribeStream(SubscribeID(1), mockStream, tt.initialConfig, nil)

			err := sss.updateSubscribe(tt.newConfig)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				// Verify config was updated
				updatedConfig := sss.TrackConfig()
				assert.Equal(t, tt.newConfig.Priority, updatedConfig.Priority, "config should be updated")
			}

		})
	}
}
