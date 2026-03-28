package moqt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewTrackReceiver(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	info := Info{}
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, info)
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	assert.NotNil(t, receiver, "newTrackReceiver should not return nil")
	// Verify info propagation
	assert.Equal(t, info, substr.ReadInfo(), "sendSubscribeStream should return the Info passed at construction")
	assert.NotNil(t, receiver.queueing, "queue should be initialized")
	assert.NotNil(t, receiver.queuedCh, "queuedCh should be initialized")
	assert.NotNil(t, receiver.dequeued, "dequeued should be initialized")
}

func TestTrackReceiver_AcceptGroup(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	// Test with a timeout to ensure we don't block forever when no groups are available
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReceiver_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(ctx)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
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

func TestTrackReceiver_EnqueueGroup(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
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

func TestTrackReceiver_AcceptGroup_RealImplementation(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
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
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
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
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	newTrackConfig := TrackConfig{}

	_ = receiver.Update(&newTrackConfig)

	// Verify update
	assert.Equal(t, &TrackConfig{}, receiver.TrackConfig())
}

func TestTrackReader_CloseWithError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil)
	mockStream.On("CancelRead", mock.Anything).Return(nil)
	mockStream.On("CancelWrite", mock.Anything).Return(nil)
	mockStream.On("Write", mock.Anything).Return(0, nil)
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	err := receiver.CloseWithError(InternalSubscribeErrorCode)
	assert.NoError(t, err)
}

func TestTrackReader_RemoveGroup(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &TrackConfig{}, Info{})
	receiver := newTrackReader("/broadcastpath", "trackname", substr, func() {})

	// Add a group to dequeued
	group := &GroupReader{}
	receiver.dequeued[group] = struct{}{}
	assert.Contains(t, receiver.dequeued, group)

	// Remove the group
	receiver.removeGroup(group)
	assert.NotContains(t, receiver.dequeued, group)
}
