package moqt

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTrackReader(tb testing.TB) (*TrackReader, *FakeQUICStream) {
	tb.Helper()
	mockStream := &FakeQUICStream{}

	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})
	return receiver, mockStream
}

func TestNewTrackReader(t *testing.T) {
	mockStream := &FakeQUICStream{}
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	assert.NotNil(t, receiver, "newTrackReader should not return nil")
	assert.Equal(t, BroadcastPath("/test"), receiver.BroadcastPath)
	assert.Equal(t, TrackName("video"), receiver.TrackName)
	// Verify info propagation
	assert.Equal(t, PublishInfo{}, substr.ReadInfo(), "sendSubscribeStream should return the Info passed at construction")
	assert.NotNil(t, receiver.queueing, "queue should be initialized")
	assert.NotNil(t, receiver.queuedCh, "queuedCh should be initialized")
	assert.NotNil(t, receiver.dequeued, "dequeued should be initialized")
}

func TestTrackReader_AcceptGroup(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Test with a timeout to ensure we don't block forever when no groups are available
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockStream := &FakeQUICStream{
		ParentCtx: ctx,
	}
	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	// Cancel the context
	cancel()

	// Test that AcceptGroup returns context error when context is cancelled
	testCtx, testCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer testCancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected error when context is cancelled")
	// Should return context.Canceled or DeadlineExceeded
	assert.True(t, err == context.Canceled || err == context.DeadlineExceeded, "expected context error")
}

func TestTrackReader_Context_FollowsStreamLifecycle(t *testing.T) {
	_, cancelSetup := context.WithCancel(context.Background())
	defer cancelSetup()

	mockStream := &FakeQUICStream{}

	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	// Cancel setup context; TrackReader context should remain alive while stream is alive.
	cancelSetup()
	select {
	case <-receiver.Context().Done():
		t.Fatal("track reader context should not be canceled by request setup context")
	case <-time.After(20 * time.Millisecond):
		// expected
	}

	// Close stream; TrackReader context should be canceled.
	require.NoError(t, mockStream.Close())

	select {
	case <-receiver.Context().Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("track reader context should be canceled when stream is closed")
	}
}

func TestTrackReader_EnqueueGroup(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Mock receive stream
	mockReceiveStream := &FakeQUICReceiveStream{}

	// Enqueue a group
	receiver.enqueueGroup(GroupSequence(1), mockReceiveStream)

	// Test that we can accept the enqueued group
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	group, err := receiver.AcceptGroup(testCtx)
	assert.NoError(t, err, "should be able to accept enqueued group")
	assert.NotNil(t, group, "accepted group should not be nil")

}

func TestTrackReader_AcceptGroup_RealImplementation(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Test with a timeout to ensure we don't block forever
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_Close(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	err := receiver.Close()
	assert.NoError(t, err)

	// Close again should not error
	err = receiver.Close()
	assert.NoError(t, err)
}

func TestTrackReader_Update(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	newTrackConfig := SubscribeConfig{}

	_ = receiver.Update(&newTrackConfig)

	// Verify update
	assert.Equal(t, &SubscribeConfig{}, receiver.TrackConfig())
}

func TestTrackReader_AcceptDrop(t *testing.T) {
	var buf bytes.Buffer
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeDrop)})
	require.NoError(t, (message.SubscribeDropMessage{
		StartGroup: 11,
		EndGroup:   21,
		ErrorCode:  3,
	}).Encode(&buf))

	mockStream := &FakeQUICStream{
		ReadFunc: buf.Read,
	}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	drop, err := receiver.acceptDrop(ctx)
	require.NoError(t, err)
	assert.Equal(t, SubscribeDrop{
		StartGroup: 10,
		EndGroup:   20,
		ErrorCode:  3,
	}, drop)
}

func TestTrackReader_CloseWithError(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	receiver.CloseWithError(SubscribeErrorCodeInternal)
}

func TestGroupReader_CancelRead_RemovesFromManager(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	recvStream := &FakeQUICReceiveStream{}
	group := newGroupReader(GroupSequence(1), recvStream, receiver.groupManager)

	assert.Len(t, receiver.groupManager.activeGroups, 1)
	assert.Contains(t, receiver.groupManager.activeGroups, group)

	group.CancelRead(InternalGroupErrorCode)
	assert.Len(t, receiver.groupManager.activeGroups, 0)
	assert.NotContains(t, receiver.groupManager.activeGroups, group)
}

func TestTrackReader_Drops(t *testing.T) {
	var buf bytes.Buffer

	// Write a SUBSCRIBE_DROP response (readSubscribeResponses returns after one drop)
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeDrop)})
	require.NoError(t, (message.SubscribeDropMessage{
		StartGroup: 11, // wire value 11 → groupSequenceFromWire → GroupSequence(10)
		EndGroup:   21, // wire value 21 → groupSequenceFromWire → GroupSequence(20)
		ErrorCode:  3,
	}).Encode(&buf))

	mockStream := &FakeQUICStream{
		ReadFunc: buf.Read,
	}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var drops []SubscribeDrop
	for drop := range receiver.Drops(ctx) {
		drops = append(drops, drop)
	}

	require.Len(t, drops, 1)
	assert.Equal(t, GroupSequence(10), drops[0].StartGroup)
	assert.Equal(t, GroupSequence(20), drops[0].EndGroup)
	assert.Equal(t, SubscribeErrorCode(3), drops[0].ErrorCode)
}

func TestTrackReader_Drops_ContextCanceled(t *testing.T) {
	mockStream := &FakeQUICStream{}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var drops []SubscribeDrop
	for drop := range receiver.Drops(ctx) {
		drops = append(drops, drop)
	}

	assert.Empty(t, drops)
}

func TestTrackReader_Update_NilConfig(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	err := receiver.Update(nil)
	assert.Error(t, err)
}

func TestTrackReader_SubscribeID(t *testing.T) {
	mockStream := &FakeQUICStream{}

	substr := newSendSubscribeStream(SubscribeID(42), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	assert.Equal(t, SubscribeID(42), receiver.SubscribeID())
}
