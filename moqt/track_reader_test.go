package moqt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewTrackReader(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	info := PublishInfo{}
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, info)
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	assert.NotNil(t, receiver, "newTrackReader should not return nil")
	// Verify info propagation
	assert.Equal(t, info, substr.ReadInfo(), "sendSubscribeStream should return the Info passed at construction")
	assert.NotNil(t, receiver.queueing, "queue should be initialized")
	assert.NotNil(t, receiver.queuedCh, "queuedCh should be initialized")
	assert.NotNil(t, receiver.dequeued, "dequeued should be initialized")
}

func TestTrackReader_AcceptGroup(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	// Test with a timeout to ensure we don't block forever when no groups are available
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(ctx)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

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

func TestTrackReader_EnqueueGroup(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	// Mock receive stream
	mockReceiveStream := &MockQUICReceiveStream{}

	// Enqueue a group
	receiver.enqueueGroup(GroupSequence(1), mockReceiveStream)

	// Test that we can accept the enqueued group
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	group, err := receiver.AcceptGroup(testCtx)
	assert.NoError(t, err, "should be able to accept enqueued group")
	assert.NotNil(t, group, "accepted group should not be nil")

	mockReceiveStream.AssertExpectations(t)
}

func TestTrackReader_AcceptGroup_RealImplementation(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	// Test with a timeout to ensure we don't block forever
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_Close(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil)
	mockStream.On("CancelRead", mock.Anything).Return(nil)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	err := receiver.Close()
	assert.NoError(t, err)

	// Close again should not error
	err = receiver.Close()
	assert.NoError(t, err)
}

func TestTrackReader_Update(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Write", mock.Anything).Return(0, nil)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	newTrackConfig := SubscribeConfig{}

	_ = receiver.Update(&newTrackConfig)

	// Verify update
	assert.Equal(t, &SubscribeConfig{}, receiver.TrackConfig())
}

func TestTrackReader_CloseWithError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil)
	mockStream.On("CancelRead", mock.Anything).Return(nil)
	mockStream.On("CancelWrite", mock.Anything).Return(nil)
	mockStream.On("Write", mock.Anything).Return(0, nil)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	err := receiver.CloseWithError(SubscribeErrorCodeInternal)
	assert.NoError(t, err)
}

func TestGroupReader_CancelRead_RemovesFromManager(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}, PublishInfo{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	recvStream := &MockQUICReceiveStream{}
	recvStream.On("CancelRead", mock.Anything).Return()
	group := newGroupReader(GroupSequence(1), recvStream, receiver.groupManager)

	assert.Len(t, receiver.groupManager.activeGroups, 1)
	assert.Contains(t, receiver.groupManager.activeGroups, group)

	group.CancelRead(InternalGroupErrorCode)
	assert.Len(t, receiver.groupManager.activeGroups, 0)
	assert.NotContains(t, receiver.groupManager.activeGroups, group)
}
