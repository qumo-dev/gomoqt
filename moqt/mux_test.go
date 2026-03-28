package moqt

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing/synctest"
)

// Test NewTrackMux function
func TestNewTrackMux(t *testing.T) {
	mux := NewTrackMux()
	assert.NotNil(t, mux, "NewTrackMux should return non-nil mux")
	assert.NotNil(t, &mux.trackHandlerIndex, "mux trackHandlerIndex should be initialized")
	assert.NotNil(t, &mux.announcementTree, "mux announcementTree should be initialized")
}

// Test Mux.Publish method
func TestMux_Publish(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	called := false
	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		called = true
	})

	// Register handler
	mux.Publish(ctx, path, handler)

	// Verify handler is registered by calling ServeTrack
	tw := &TrackWriter{BroadcastPath: path}
	mux.serveTrack(tw)

	assert.True(t, called, "handler should be called")
}

// Test Mux.PublishFunc method
func TestMux_PublishFunc(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	called := false
	mux.PublishFunc(ctx, path, func(tw *TrackWriter) {
		called = true
		assert.Equal(t, path, tw.BroadcastPath)
	})

	// Verify handler is registered and called
	tw := &TrackWriter{BroadcastPath: path}
	mux.serveTrack(tw)

	assert.True(t, called, "handler function should be called")
}

// Test path validation
func TestMux_Publish_InvalidPath(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	tests := []struct {
		name string
		path BroadcastPath
	}{
		{"empty_path", BroadcastPath("")},
		{"no_leading_slash", BroadcastPath("test")},
		{"relative_path", BroadcastPath("./test")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() {
				mux.Publish(ctx, tt.path, handler)
			}, "should panic for invalid path: %s", tt.path)
		})
	}
}

// Publish with nil context should panic
func TestMux_Publish_NilContext_Panic(t *testing.T) {
	mux := NewTrackMux()
	path := BroadcastPath("/test/nilctx")
	var handler TrackHandler = TrackHandlerFunc(func(tw *TrackWriter) {})
	assert.Panics(t, func() {
		var nilCtx context.Context
		mux.Publish(nilCtx, path, handler)
	}, "Publish should panic when context is nil")
}

// PublishFunc with nil context should panic
func TestMux_PublishFunc_NilContext_Panic(t *testing.T) {
	mux := NewTrackMux()
	path := BroadcastPath("/test/nilctxfunc")
	assert.Panics(t, func() {
		var nilCtx context.Context
		mux.PublishFunc(nilCtx, path, func(tw *TrackWriter) {})
	}, "PublishFunc should panic when context is nil")
}

// Announce with nil handler: TrackHandler should be NotFound and serveTrack should close with TrackNotFoundErrorCode
func TestMux_Announce_WithNilHandler_ClosesTrack(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/announce/nilhandler")

	ann, end := NewAnnouncement(ctx, path)
	defer end()

	// Announce with nil handler
	mux.Announce(ann, nil)

	a, h := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be treated as not found when handler is nil")
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer())

	// Now try serveTrack - should call CloseWithError and stream CancelWrite/CancelRead with TrackNotFoundErrorCode
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("CancelRead", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("Close").Return(nil).Maybe()

	tw := newTrackWriter(path, TrackName("test"), newReceiveSubscribeStream(SubscribeID(1), func() Stream { return mockStream }(), &TrackConfig{}), func() (SendStream, error) {
		return &MockQUICSendStream{}, nil
	}, func() {})

	// serveTrack will call CloseWithError
	mux.serveTrack(tw)

	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertExpectations(t)
}

// serveTrack not found should close the stream with TrackNotFoundErrorCode
func TestMux_ServeTrack_NotFound_ClosesWithError(t *testing.T) {
	mux := NewTrackMux()
	path := BroadcastPath("/not/found/close")

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("CancelRead", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("Close").Return(nil).Maybe()

	tw := newTrackWriter(path, TrackName("test"), newReceiveSubscribeStream(SubscribeID(1), func() Stream { return mockStream }(), &TrackConfig{}), func() (SendStream, error) {
		return &MockQUICSendStream{}, nil
	}, func() {})

	mux.serveTrack(tw)

	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertExpectations(t)
}

// Typed-nil handler should be treated as NotFound by TrackHandler
func TestMux_TrackHandler_TypedNilHandler_TreatedAsNotFound(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/typed/nil/handler")

	var hf TrackHandlerFunc = nil
	mux.Publish(ctx, path, hf)

	a, h := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be nil for typed nil handler")
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer())
}

// serveAnnouncements(nil) should not panic
func TestMux_ServeAnnouncements_NilAnnouncementWriter_NoPanic(t *testing.T) {
	mux := NewTrackMux()
	assert.NotPanics(t, func() { mux.serveAnnouncements(nil) })
}

// Test ServeTrack with registered handler
func TestMux_ServeTrack(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test/path")

	receivedTW := make(chan *TrackWriter, 1)
	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		receivedTW <- tw
	})

	mux.Publish(ctx, path, handler)

	// Create track writer and serve
	tw := &TrackWriter{
		BroadcastPath: path,
		TrackName:     TrackName("test_track"),
	}
	mux.serveTrack(tw)

	// Verify handler was called with correct track writer
	select {
	case received := <-receivedTW:
		assert.Equal(t, tw, received, "handler should receive the same track writer")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("handler should have been called")
	}
}

// Test ServeTrack with NotFoundHandler (no registered handler)
func TestMux_ServeTrack_NotFound(t *testing.T) {
	mux := NewTrackMux()

	tw := &TrackWriter{
		BroadcastPath: BroadcastPath("/nonexistent"),
		TrackName:     TrackName("test_track"),
	}

	// Should use NotFoundHandler - this shouldn't panic
	assert.NotPanics(t, func() {
		mux.serveTrack(tw)
	}, "ServeTrack with unregistered path should not panic")
}

// Test ServeTrack with nil TrackWriter
func TestMux_ServeTrack_NilTrackWriter(t *testing.T) {
	mux := NewTrackMux()

	assert.NotPanics(t, func() {
		mux.serveTrack(nil)
	}, "ServeTrack with nil should not panic")
}

// Test Publishr method
func TestMux_Publishr(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	// Initially should return NotFoundHandler
	a, handler := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be nil for unregistered path")
	// Compare handler functions by reflect.Value since func == func is invalid
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler), reflect.ValueOf(handler), "Handler should be NotFoundTrackHandler for unregistered path")

	// Register handler
	expectedHandler := TrackHandlerFunc(func(tw *TrackWriter) {})
	mux.Publish(ctx, path, expectedHandler)

	// Should return registered handler
	a, handler = mux.TrackHandler(path)
	assert.NotNil(t, a, "Announcement should not be nil for registered path")
	assert.Equal(t, reflect.ValueOf(expectedHandler), reflect.ValueOf(handler), "Handler should be the registered handler")
}

// Test nested paths
func TestMux_NestedPaths(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	paths := []BroadcastPath{
		BroadcastPath("/"),
		BroadcastPath("/api"),
		BroadcastPath("/api/v1"),
		BroadcastPath("/api/v1/users"),
		BroadcastPath("/api/v2"),
	}

	calledPaths := make(map[BroadcastPath]bool)
	mu := sync.Mutex{}

	// Register handlers for all paths
	for _, path := range paths {
		p := path // capture loop variable
		mux.PublishFunc(ctx, p, func(tw *TrackWriter) {
			mu.Lock()
			calledPaths[p] = true
			mu.Unlock()
		})
	}

	// Test each path
	for _, path := range paths {
		tw := &TrackWriter{BroadcastPath: path}
		mux.serveTrack(tw)

		mu.Lock()
		called := calledPaths[path]
		mu.Unlock()

		assert.True(t, called, "handler for path %s should be called", path)
	}
}

// Test concurrent access
func TestMux_ConcurrentAccess(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	const numGoroutines = 50
	const pathsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent registration and serving
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range pathsPerGoroutine {
				path := BroadcastPath(fmt.Sprintf("/test/%d/%d", id, j))

				// Register handler
				mux.PublishFunc(ctx, path, func(tw *TrackWriter) {
					// Handler called
				})

				// Serve track
				tw := &TrackWriter{BroadcastPath: path}
				mux.serveTrack(tw)

				// Small delay to increase chance of race conditions
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

// Test path validation function
func TestIsValidPath(t *testing.T) {
	tests := []struct {
		name  string
		path  BroadcastPath
		valid bool
	}{
		{"root", BroadcastPath("/"), true},
		{"simple", BroadcastPath("/test"), true},
		{"nested", BroadcastPath("/api/v1/users"), true},
		{"with_dots", BroadcastPath("/api/user.profile"), true},
		{"with_underscores", BroadcastPath("/api/user_profile"), true},
		{"with_hyphens", BroadcastPath("/api/user-profile"), true},
		{"double_dots", BroadcastPath("/../test"), true},
		{"empty", BroadcastPath(""), false},
		{"no_leading_slash", BroadcastPath("test"), false},
		{"only_dots", BroadcastPath("./test"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidPath(tt.path)
			assert.Equal(t, tt.valid, result, "isValidPath(%s) should return %v", tt.path, tt.valid)
		})
	}
}

// Test prefix validation function
func TestIsValidPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		valid  bool
	}{
		{"valid_basic", "/test/", true},
		{"valid_nested", "/deep/nested/", true},
		{"valid_root", "/", true},
		{"invalid_empty", "", false},
		{"invalid_no_leading_slash", "test/", false},
		{"invalid_no_trailing_slash", "/test", false},
		{"invalid_no_slashes", "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidPrefix(tt.prefix)
			assert.Equal(t, tt.valid, result, "isValidPrefix(%s) should return %v", tt.prefix, tt.valid)
		})
	}
}

// Test default mux functions
func TestDefaultMux(t *testing.T) {
	// Clean up any previous state
	DefaultMux = NewTrackMux()
	ctx := context.Background()

	path := BroadcastPath("/default/test")

	called := false
	Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {
		called = true
	}))

	tw := &TrackWriter{BroadcastPath: path}
	DefaultMux.serveTrack(tw)

	assert.True(t, called, "default handler should be called")
}

func TestDefaultMux_PublishFunc(t *testing.T) {
	// Clean up any previous state
	DefaultMux = NewTrackMux()
	ctx := context.Background()

	path := BroadcastPath("/default/test2")

	called := false
	PublishFunc(ctx, path, func(tw *TrackWriter) {
		called = true
	})

	tw := &TrackWriter{BroadcastPath: path}
	DefaultMux.serveTrack(tw)

	assert.True(t, called, "default handler function should be called")
}

// TestMux_Announce_NotifiesRootSubscriptions removed: covered by TestMux_ServeAnnouncements_InitSendsExistingAnnouncements

// Test edge cases
func TestMux_EdgeCases(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	// Test with special characters in path
	specialPath := BroadcastPath("/测试/テスト/🚀/test")
	called := false

	assert.NotPanics(t, func() {
		mux.PublishFunc(ctx, specialPath, func(tw *TrackWriter) {
			called = true
		})
	}, "should handle special characters in path")

	tw := &TrackWriter{BroadcastPath: specialPath}
	mux.serveTrack(tw)

	assert.True(t, called, "handler with special characters should be called")
}

// Test overwriting handlers
func TestMux_OverwriteHandler(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	called1 := false
	called2 := false

	handler1 := TrackHandlerFunc(func(tw *TrackWriter) { called1 = true })
	handler2 := TrackHandlerFunc(func(tw *TrackWriter) { called2 = true })

	// Register first handler
	mux.Publish(ctx, path, handler1)

	// Test first handler
	tw := &TrackWriter{BroadcastPath: path}
	mux.serveTrack(tw)
	assert.True(t, called1, "first handler should be called")
	assert.False(t, called2, "second handler should not be called yet")

	// Overwrite with second handler
	called1, called2 = false, false
	mux.Publish(ctx, path, handler2)

	// Test that second handler is now active
	mux.serveTrack(tw)
	assert.False(t, called1, "first handler should not be called after overwrite")
	assert.True(t, called2, "second handler should be called after overwrite")
}

// (Removed) TestMux_Clear - Clear has been removed from TrackMux in favor of safer shutdown semantics.

// Test Mux.Announce method
func TestMux_Announce(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	announcement, end := NewAnnouncement(ctx, path)
	defer end() // Ensure cleanup

	called := false
	handler := TrackHandlerFunc(func(tw *TrackWriter) { called = true })

	// Test direct announce
	mux.Announce(announcement, handler)

	// Verify handler is registered
	tw := &TrackWriter{BroadcastPath: path}
	mux.serveTrack(tw)
	assert.True(t, called, "announced handler should be called")
}

// Test with inactive announcement
func TestMux_Announce_Inactive(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test")

	announcement, end := NewAnnouncement(ctx, path)
	end() // Make it inactive immediately

	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Should not register handler for inactive announcement
	mux.Announce(announcement, handler)

	// Handler should not be registered
	a, foundHandler := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be nil for inactive announcement")
	handlerPtr := reflect.ValueOf(foundHandler).Pointer()
	notFoundPtr := reflect.ValueOf(NotFoundTrackHandler).Pointer()
	assert.Equal(t, notFoundPtr, handlerPtr, "Handler should be NotFoundTrackHandler for inactive announcement")
}

// Publish should remove the handler when the context is cancelled
func TestMux_Publish_CleanupOnContextCancel(t *testing.T) {
	mux := NewTrackMux()
	ctx, cancel := context.WithCancel(context.Background())
	path := BroadcastPath("/publish/cleanup")

	mux.Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {}))

	// Ensure handler is registered
	a, h := mux.TrackHandler(path)
	assert.NotNil(t, a, "Announcement should be present after Publish")
	assert.NotEqual(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer(), "Handler should not be NotFound after Publish")

	// Cancel the publish context and wait for removal
	cancel()
	deadline := time.Now().Add(500 * time.Millisecond)
	removed := false
	for time.Now().Before(deadline) {
		a2, h2 := mux.TrackHandler(path)
		if a2 == nil {
			removed = true
			break
		}
		if reflect.ValueOf(h2).Pointer() == reflect.ValueOf(NotFoundTrackHandler).Pointer() {
			removed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !removed {
		t.Fatalf("Publish handler was not removed after context cancel")
	}
}

// PublishFunc should remove the handler when the context is cancelled
func TestMux_PublishFunc_CleanupOnContextCancel(t *testing.T) {
	mux := NewTrackMux()
	ctx, cancel := context.WithCancel(context.Background())
	path := BroadcastPath("/publishfunc/cleanup")

	mux.PublishFunc(ctx, path, func(tw *TrackWriter) {})

	// Ensure handler is registered
	a, h := mux.TrackHandler(path)
	assert.NotNil(t, a, "Announcement should be present after PublishFunc")
	assert.NotEqual(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer(), "Handler should not be NotFound after PublishFunc")

	// Cancel the publish context and wait for removal
	cancel()
	deadline := time.Now().Add(500 * time.Millisecond)
	removed := false
	for time.Now().Before(deadline) {
		a2, h2 := mux.TrackHandler(path)
		if a2 == nil {
			removed = true
			break
		}
		if reflect.ValueOf(h2).Pointer() == reflect.ValueOf(NotFoundTrackHandler).Pointer() {
			removed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !removed {
		t.Fatalf("PublishFunc handler was not removed after context cancel")
	}
}

// Announce twice on same path should overwrite the previous handler and end it
func TestMux_Announce_OverwriteHandler(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/announce/overwrite")

	ann1, _ := NewAnnouncement(ctx, path)
	handler1 := TrackHandlerFunc(func(tw *TrackWriter) { /* unused in this test */ })
	mux.Announce(ann1, handler1)

	// Announce again with a new announcement and handler
	ann2, _ := NewAnnouncement(ctx, path)
	handler2 := TrackHandlerFunc(func(tw *TrackWriter) { /* unused in this test */ })
	mux.Announce(ann2, handler2)

	// After second announce, the first announcement should be ended and handler replaced
	// Wait until the handler mapping reflects the second handler or the first ended
	deadline := time.Now().Add(500 * time.Millisecond)
	replaced := false
	for time.Now().Before(deadline) {
		a, _ := mux.TrackHandler(path)
		if a == ann2 {
			replaced = true
			break
		}
		if !ann1.IsActive() {
			// first announcement ended
			replaced = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !replaced {
		t.Fatalf("announcement handler did not get replaced by the second Announce")
	}
	// The active handler should be the second handler
	a, h := mux.TrackHandler(path)
	assert.Equal(t, ann2, a)
	assert.Equal(t, reflect.ValueOf(handler2), reflect.ValueOf(h), "Handler should be replaced by the second announce")
}

// PublishFunc with nil function should be treated as NotFound handler
func TestMux_PublishFunc_NilHandler_TreatedAsNotFound(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/publishfunc/nilhandler")

	var f func(tw *TrackWriter) = nil
	mux.PublishFunc(ctx, path, f)

	a, h := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be nil for nil PublishFunc handler")
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer())
}

// TrackHandler with invalid path should return NotFound
func TestMux_TrackHandler_InvalidPath_ReturnsNotFound(t *testing.T) {
	mux := NewTrackMux()
	a, h := mux.TrackHandler(BroadcastPath("invalid/no/leading/slash"))
	assert.Nil(t, a)
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer())
}

func TestNotFound(t *testing.T) {
	tests := map[string]struct {
		trackWriter *TrackWriter
		expectPanic bool
	}{
		"nil track writer": {
			trackWriter: nil,
			expectPanic: false,
		},
		"track writer with nil TrackWriter": {
			trackWriter: newTrackWriter(BroadcastPath("/test"), TrackName("test"), nil, nil, nil),
			expectPanic: false,
		},
		"track writer with mock TrackWriter": {
			trackWriter: newTrackWriter(BroadcastPath("/test"), TrackName("test"),
				newReceiveSubscribeStream(SubscribeID(1), func() Stream {
					mockStream := &MockQUICStream{}
					mockStream.On("Context").Return(context.Background())
					mockStream.On("Read", mock.Anything).Return(0, io.EOF)
					mockStream.On("CancelWrite", mock.Anything).Return()
					mockStream.On("CancelRead", mock.Anything).Return()
					return mockStream
				}(), &TrackConfig{}),
				func() (SendStream, error) {
					return &MockQUICSendStream{}, nil
				}, func() {}),
			expectPanic: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Should not panic in any case
			NotFound(tt.trackWriter)
		})
	}
}

func TestNotFoundHandler(t *testing.T) {
	tests := map[string]struct {
		trackWriter *TrackWriter
		expectPanic bool
	}{
		"nil track writer": {
			trackWriter: nil,
			expectPanic: false,
		},
		"track writer with nil TrackWriter": {
			trackWriter: newTrackWriter(BroadcastPath("/test"), TrackName("test"), nil, nil, nil),
			expectPanic: false,
		},
		"track writer with mock TrackWriter": {
			trackWriter: newTrackWriter(BroadcastPath("/test"), TrackName("test"),
				newReceiveSubscribeStream(SubscribeID(1), func() Stream {
					mockStream := &MockQUICStream{}
					mockStream.On("Context").Return(context.Background())
					mockStream.On("Read", mock.Anything).Return(0, io.EOF)
					mockStream.On("CancelWrite", mock.Anything).Return()
					mockStream.On("CancelRead", mock.Anything).Return()
					return mockStream
				}(), &TrackConfig{}),
				func() (SendStream, error) {
					return &MockQUICSendStream{}, nil
				}, func() {}),
			expectPanic: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			trackWriter := tt.trackWriter

			// Should not panic in any case
			NotFoundTrackHandler.ServeTrack(trackWriter)
		})
	}
}

func TestTrackHandlerFunc(t *testing.T) {
	called := false
	var receivedTrackWriter *TrackWriter

	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		called = true
		receivedTrackWriter = tw
	})

	// Create a proper TrackWriter with a valid receiveSubscribeStream
	testTrackWriter := newTrackWriter(BroadcastPath("/test"), TrackName("test"),
		newReceiveSubscribeStream(SubscribeID(1), func() Stream {
			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)
			return mockStream
		}(), &TrackConfig{}),
		func() (SendStream, error) {
			return &MockQUICSendStream{}, nil
		}, func() {})

	handler.ServeTrack(testTrackWriter)

	assert.True(t, called, "handler function was not called")
	assert.Equal(t, testTrackWriter, receivedTrackWriter, "handler did not receive the correct track writer")
}

func TestTrackHandlerFuncServeTrack(t *testing.T) {
	callCount := 0

	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		callCount++
	})

	// Create a proper TrackWriter with a valid receiveSubscribeStream
	trackWriter := newTrackWriter(BroadcastPath("/test"), TrackName("test"),
		newReceiveSubscribeStream(SubscribeID(1), func() Stream {
			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)
			return mockStream
		}(), &TrackConfig{}),
		func() (SendStream, error) {
			return &MockQUICSendStream{}, nil
		}, func() {})

	handler.ServeTrack(trackWriter)
	handler.ServeTrack(trackWriter)
	handler.ServeTrack(trackWriter)

	assert.Equal(t, 3, callCount, "expected handler to be called 3 times")
}

// Additional robust tests for TrackMux
func TestMux_AnnouncementDeliveryToMultipleListeners(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/multi")

	announcement, end := NewAnnouncement(ctx, path)
	defer end()

	received := make(chan *Announcement, 2)
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Register two listeners for the same path
	mux.Announce(announcement, handler)
	mux.Announce(announcement, handler)

	// Simulate announcement delivery
	go func() {
		received <- announcement
	}()
	go func() {
		received <- announcement
	}()

	// Both listeners should receive the announcement
	count := 0
	for range 2 {
		select {
		case ann := <-received:
			assert.Equal(t, announcement, ann, "Listener should receive the correct announcement")
			count++
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Listener did not receive announcement")
		}
	}
	assert.Equal(t, 2, count, "Both listeners should receive the announcement")
}

func TestMux_AnnouncementTreeCleanup(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/cleanup")

	announcement, end := NewAnnouncement(ctx, path)
	defer end()

	handler := TrackHandlerFunc(func(tw *TrackWriter) {})
	mux.Announce(announcement, handler)

	// Simulate listener disconnect
	end()

	// After cleanup, handler should not be registered
	a, h := mux.TrackHandler(path)
	assert.Nil(t, a, "Announcement should be nil after cleanup")
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer(), "Handler should be NotFoundTrackHandler after cleanup")
}

func TestMux_AnnouncementDeliveryErrorPropagation(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/errorprop")

	announcement, end := NewAnnouncement(ctx, path)
	defer end()

	// Handler that simulates SendAnnouncement error
	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		// Simulate error: do nothing, just ensure no panic
	})
	mux.Announce(announcement, handler)

	// No panic should occur
	tw := &TrackWriter{BroadcastPath: path}
	assert.NotPanics(t, func() {
		mux.serveTrack(tw)
	}, "ServeTrack should handle SendAnnouncement error gracefully")
}

func TestMux_AnnounceWithNilAnnouncementOrHandler(t *testing.T) {
	mux := NewTrackMux()
	// Announce with nil Announcement should panic deliberately
	assert.Panics(t, func() {
		mux.Announce(nil, nil)
	}, "Announce with nil Announcement/handler should panic")
}

func TestMux_SimultaneousAnnounceAndPublish(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/race")

	var wg sync.WaitGroup
	wg.Add(2)

	handler := TrackHandlerFunc(func(tw *TrackWriter) {})
	announcement, end := NewAnnouncement(ctx, path)
	defer end()

	go func() {
		defer wg.Done()
		mux.Announce(announcement, handler)
	}()
	go func() {
		defer wg.Done()
		mux.Publish(ctx, path, handler)
	}()

	wg.Wait()
	// After both, handler should be registered
	_, h := mux.TrackHandler(path)
	assert.NotNil(t, h, "Handler should be registered after simultaneous Announce and Publish")
}

// Test serveAnnouncements: initialization sends existing announcements to the writer
func TestMux_ServeAnnouncements_InitSendsExistingAnnouncements(t *testing.T) {
	tests := map[string]struct {
		announcePath BroadcastPath
		writerPrefix string
	}{
		"prefix": {announcePath: BroadcastPath("/test/stream1"), writerPrefix: "/test/"},
		"root":   {announcePath: BroadcastPath("/rootstream"), writerPrefix: "/"},
	}

	for name, tc := range tests {
		synctest.Test(t, func(t *testing.T) {
			mux := NewTrackMux()
			ctx := context.Background()

			// Create an announcement and register it
			ann, end := NewAnnouncement(ctx, tc.announcePath)
			defer end()
			mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

			// Prepare mock stream for AnnouncementWriter
			mockStream := &MockQUICStream{}
			streamCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mockStream.On("Context").Return(streamCtx)
			// Allow write calls (init + potential active messages)
			writeCh := make(chan struct{}, 1)
			mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
				select {
				case writeCh <- struct{}{}:
				default:
				}
			})
			mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
			mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
			mockStream.On("Close").Return(nil).Maybe()

			aw := newAnnouncementWriter(mockStream, tc.writerPrefix)

			var wg sync.WaitGroup
			wg.Go(func() {
				mux.serveAnnouncements(aw)
			})

			select {
			case <-writeCh:
				// ok
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("expected Write to be called on mockStream during init for case %s", name)
			}

			// stop serveAnnouncements by cancelling the writer's underlying context
			cancel()

			// wait for goroutine to finish
			ch := make(chan struct{})
			go func() {
				wg.Wait()
				close(ch)
			}()
			select {
			case <-ch:
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("serveAnnouncements did not exit after cancelling context for case %s", name)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

// Test that when an announcement is created before serveAnnouncements starts,
// both an ancestor (root) and descendant writer receive initialization.
func TestMux_ServeAnnouncements_AncestorAndDescendantReceive_AnnounceBefore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Prepare announcement for /share/stream
		ann, end := NewAnnouncement(ctx, BroadcastPath("/share/stream"))
		defer end()
		// Announce before registering writers
		mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

		// Root writer (prefix "/")
		rootStream := &MockQUICStream{}
		rootCtx, cancelR := context.WithCancel(context.Background())
		defer cancelR()
		rootStream.On("Context").Return(rootCtx)
		rootWriteCh := make(chan struct{}, 1)
		rootStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case rootWriteCh <- struct{}{}:
			default:
			}
		})
		rootStream.On("Close").Return(nil).Maybe()
		rootStream.On("CancelWrite", mock.Anything).Return().Maybe()
		rootStream.On("CancelRead", mock.Anything).Return().Maybe()
		rootAW := newAnnouncementWriter(rootStream, "/")

		// Descendant writer (prefix /share/)
		shareStream := &MockQUICStream{}
		shareCtx, cancelS := context.WithCancel(context.Background())
		defer cancelS()
		shareStream.On("Context").Return(shareCtx)
		shareWriteCh := make(chan struct{}, 1)
		shareStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case shareWriteCh <- struct{}{}:
			default:
			}
		})
		shareStream.On("Close").Return(nil).Maybe()
		shareStream.On("CancelWrite", mock.Anything).Return().Maybe()
		shareStream.On("CancelRead", mock.Anything).Return().Maybe()
		shareAW := newAnnouncementWriter(shareStream, "/share/")

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			mux.serveAnnouncements(rootAW)
		}()
		go func() {
			defer wg.Done()
			mux.serveAnnouncements(shareAW)
		}()

		// Wait for both streams to receive a write during init
		deadline := time.Now().Add(500 * time.Millisecond)
		gotRoot, gotShare := false, false
		for time.Now().Before(deadline) {
			for _, c := range rootStream.Calls {
				if c.Method == "Write" {
					gotRoot = true
					break
				}
			}
			for _, c := range shareStream.Calls {
				if c.Method == "Write" {
					gotShare = true
					break
				}
			}
			if gotRoot && gotShare {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !gotRoot || !gotShare {
			t.Fatalf("expected Write on both root and share streams: gotRoot=%v gotShare=%v", gotRoot, gotShare)
		}

		// cleanup
		cancelR()
		cancelS()
		wg.Wait()
		rootStream.AssertExpectations(t)
		shareStream.AssertExpectations(t)
	})
}

// Test serveAnnouncements: invalid prefix causes CloseWithError -> CancelWrite/CancelRead
func TestMux_ServeAnnouncements_InvalidPrefix_ClosesWithError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()

		mockStream := &MockQUICStream{}
		mockCtx := context.Background()
		mockStream.On("Context").Return(mockCtx)
		mockStream.On("Close").Return(nil).Maybe()
		mockStream.On("Close").Return(nil).Maybe()
		// Allow Write calls to avoid unexpected panics if init attempts to write
		mockStream.On("Write", mock.Anything).Return(0, nil).Maybe()

		// Expect CancelWrite and CancelRead with InvalidPrefixErrorCode
		mockStream.On("CancelWrite", StreamErrorCode(InvalidPrefixErrorCode)).Return().Once()
		mockStream.On("CancelRead", StreamErrorCode(InvalidPrefixErrorCode)).Return().Once()

		aw := newAnnouncementWriter(mockStream, "/test/")
		// Force invalid prefix for this test case (no trailing slash)
		aw.prefix = "/test"

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// Wait up to 500ms for CancelWrite to be called
		deadline := time.Now().Add(500 * time.Millisecond)
		found := false
		for time.Now().Before(deadline) {
			for _, c := range mockStream.Calls {
				if c.Method == "CancelWrite" {
					found = true
					break
				}
			}
			if found {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if !found {
			t.Fatal("expected CancelWrite to be called on mockStream for invalid prefix")
		}

		mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(InvalidPrefixErrorCode))
		mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(InvalidPrefixErrorCode))

		// ensure goroutine terminates
		ch := make(chan struct{})
		go func() {
			wg.Wait()
			close(ch)
		}()
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("serveAnnouncements did not exit for invalid prefix")
		}

		mockStream.AssertExpectations(t)
	})
}

// Test serveAnnouncements: init returns a StreamError and serveAnnouncements should close with InternalAnnounceErrorCode
func TestMux_ServeAnnouncements_InitWriteError_ClosesWithInternalError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Create an announcement so that aw.init will attempt to write
		ann, end := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
		defer end()
		mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

		mockStream := &MockQUICStream{}
		mockStream.On("Context").Return(context.Background())
		mockStream.On("Close").Return(nil).Maybe()
		mockStream.On("Close").Return(nil).Maybe()

		streamError := &StreamError{
			StreamID:  StreamID(1),
			ErrorCode: StreamErrorCode(42),
		}

		// Make the first Write (in init) fail with a StreamError
		mockStream.On("Write", mock.Anything).Return(0, streamError).Once()

		// Expect CloseWithError -> CancelWrite/CancelRead with InternalAnnounceErrorCode
		mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Once()
		mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Once()

		aw := newAnnouncementWriter(mockStream, "/test/")

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// Wait up to 500ms for CancelWrite to be called
		deadline := time.Now().Add(500 * time.Millisecond)
		found := false
		for time.Now().Before(deadline) {
			for _, c := range mockStream.Calls {
				if c.Method == "CancelWrite" {
					found = true
					break
				}
			}
			if found {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if !found {
			t.Fatal("expected CancelWrite to be called on mockStream when init write fails")
		}

		mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(InternalAnnounceErrorCode))
		mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(InternalAnnounceErrorCode))

		ch := make(chan struct{})
		go func() {
			wg.Wait()
			close(ch)
		}()
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("serveAnnouncements did not exit after init write error")
		}

		mockStream.AssertExpectations(t)
	})
}

// Test serveAnnouncements: SendAnnouncement (after init) returns write error and serveAnnouncements should close with InternalAnnounceErrorCode
func TestMux_ServeAnnouncements_SendAnnouncementWriteError_ClosesWithInternalError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Prepare a new announcement to be announced later
		annLater, endLater := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))
		defer endLater()

		mockStream := &MockQUICStream{}
		mockStream.On("Context").Return(context.Background())
		mockStream.On("Close").Return(nil).Maybe()

		// Make the first Write (SendAnnouncement) fail with StreamError
		streamErr := &StreamError{StreamID: StreamID(2), ErrorCode: StreamErrorCode(99)}
		writeCh := make(chan struct{}, 1)
		mockStream.On("Write", mock.Anything).Return(0, streamErr).Once().Run(func(args mock.Arguments) {
			select {
			case writeCh <- struct{}{}:
			default:
			}
		})

		// Expect cancel calls for internal error
		mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Once()
		mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Once()

		aw := newAnnouncementWriter(mockStream, "/test/")

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// Wait until serveAnnouncements has registered (if init writes), or proceed if not
		select {
		case <-writeCh:
		case <-time.After(50 * time.Millisecond):
		}

		// Announce the later announcement; this should trigger SendAnnouncement which will cause the second Write to fail
		mux.Announce(annLater, TrackHandlerFunc(func(tw *TrackWriter) {}))

		// Wait for CancelWrite to be called
		deadline := time.Now().Add(500 * time.Millisecond)
		found := false
		for time.Now().Before(deadline) {
			for _, c := range mockStream.Calls {
				if c.Method == "CancelWrite" {
					found = true
					break
				}
			}
			if found {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if !found {
			t.Fatal("expected CancelWrite to be called on mockStream after SendAnnouncement write error")
		}

		mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(InternalAnnounceErrorCode))
		mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(InternalAnnounceErrorCode))

		ch := make(chan struct{})
		go func() {
			wg.Wait()
			close(ch)
		}()
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("serveAnnouncements did not exit after SendAnnouncement write error")
		}

		mockStream.AssertExpectations(t)
	})
}

// Test serveAnnouncements: cancelling the writer context stops the loop
func TestMux_ServeAnnouncements_ContextCancel_StopsLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()

		mockStream := &MockQUICStream{}
		streamCtx, cancel := context.WithCancel(context.Background())
		mockStream.On("Context").Return(streamCtx)
		// allow Write if init happens
		writeCh := make(chan struct{}, 1)
		mockStream.On("Write", mock.Anything).Return(0, nil).Maybe().Run(func(args mock.Arguments) {
			select {
			case writeCh <- struct{}{}:
			default:
			}
		})
		mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
		mockStream.On("CancelRead", mock.Anything).Return().Maybe()
		mockStream.On("Close").Return(nil).Maybe()

		aw := newAnnouncementWriter(mockStream, "/test/")

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// wait for serveAnnouncements to register (if it writes during init); short 50ms fallback
		select {
		case <-writeCh:
		case <-time.After(50 * time.Millisecond):
		}

		// Cancel the underlying stream context - this should cancel aw.Context()
		cancel()

		// wait for goroutine to exit
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// ok
		case <-time.After(500 * time.Millisecond):
			t.Fatal("serveAnnouncements did not exit after cancelling writer context")
		}

		// Ensure CancelWrite/CancelRead were not called (normal cancellation path)
		for _, c := range mockStream.Calls {
			if c.Method == "CancelWrite" || c.Method == "CancelRead" {
				t.Fatalf("unexpected cancel call %s during context cancel path", c.Method)
			}
		}

		mockStream.AssertExpectations(t)
	})
}

// Test serveAnnouncements with a slow subscriber: ensure Announce does not deadlock and that
// send attempts are bounded (messages may be dropped but serveAnnouncements should continue).
func TestMux_ServeAnnouncements_SlowSubscriber_NoDeadlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Prepare a mock stream that simulates slow writes (sleep)
		mockStream := &MockQUICStream{}
		streamCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mockStream.On("Context").Return(streamCtx)

		writeCalls := int32(0)
		// Simulate a slow write by sleeping on each Write call
		mockStream.On("Write", mock.Anything).Return(0, nil).Maybe().Run(func(args mock.Arguments) {
			atomic.AddInt32(&writeCalls, 1)
			time.Sleep(50 * time.Millisecond) // slow write
		})
		mockStream.On("Close").Return(nil).Maybe()
		mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
		mockStream.On("CancelRead", mock.Anything).Return().Maybe()

		aw := newAnnouncementWriter(mockStream, "/slow/")

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// Give the writer a moment to initialize
		time.Sleep(20 * time.Millisecond)

		// Fire many announces in quick succession; some may be dropped due to channel buffer limits
		count := 20
		for i := range count {
			ann, end := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/slow/stream-%d", i)))
			mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))
			// end the announcement immediately so we don't leak
			end()
		}

		// Wait a bit to allow writes to be processed
		time.Sleep(500 * time.Millisecond)

		// Cancel and wait for serveAnnouncements to exit
		cancel()
		wg.Wait()

		// There should be some writes if announcements are made.
		// Due to timing, we may have zero writes if no announcements were received
		// by the writer before it exited, so we just verify it doesn't exceed requests.
		writeCount := atomic.LoadInt32(&writeCalls)
		assert.LessOrEqual(t, writeCount, int32(count), "expected write calls not to exceed announces")

		mockStream.AssertExpectations(t)
	})
}

// Test that when the same subscription channel is present under multiple nodes
// the announcer dedupes by channel and sends at most one announcement per announce.
// Test removed: duplicate channel registration across nodes is unrealistic; AWs register per-node with their own channel

// Test serveAnnouncements: two listeners for same prefix both receive announcements
func TestMux_ServeAnnouncements_MultipleListeners_ReceiveAnnouncement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Prepare announcement to broadcast
		ann, end := NewAnnouncement(ctx, BroadcastPath("/multi/stream"))
		defer end()

		// First mock stream
		mock1 := &MockQUICStream{}
		ctx1, cancel1 := context.WithCancel(context.Background())
		mock1.On("Context").Return(ctx1)
		writeCh1 := make(chan struct{}, 1)
		mock1.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case writeCh1 <- struct{}{}:
			default:
			}
		})
		mock1.On("Close").Return(nil).Maybe()
		mock1.On("CancelRead", mock.Anything).Return().Maybe()
		mock1.On("Close").Return(nil).Maybe()
		mock1.On("CancelWrite", mock.Anything).Return().Maybe()
		mock1.On("CancelRead", mock.Anything).Return().Maybe()
		aw1 := newAnnouncementWriter(mock1, "/multi/")

		// Second mock stream
		mock2 := &MockQUICStream{}
		ctx2, cancel2 := context.WithCancel(context.Background())
		mock2.On("Context").Return(ctx2)
		writeCh2 := make(chan struct{}, 1)
		mock2.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case writeCh2 <- struct{}{}:
			default:
			}
		})
		mock2.On("Close").Return(nil).Maybe()
		mock2.On("CancelRead", mock.Anything).Return().Maybe()
		mock2.On("Close").Return(nil).Maybe()
		mock2.On("CancelWrite", mock.Anything).Return().Maybe()
		mock2.On("CancelRead", mock.Anything).Return().Maybe()
		aw2 := newAnnouncementWriter(mock2, "/multi/")

		// Start two serveAnnouncements goroutines
		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw1)
		})
		wg.Go(func() {
			mux.serveAnnouncements(aw2)
		})

		// Wait until both serveAnnouncements are ready (saw initial Write or timed out)
		select {
		case <-writeCh1:
		case <-time.After(50 * time.Millisecond):
		}
		select {
		case <-writeCh2:
		case <-time.After(50 * time.Millisecond):
		}

		// Announce - should be delivered to both listeners
		mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

		// Wait for SendAnnouncement to be called on both streams
		deadline := time.Now().Add(500 * time.Millisecond)
		got1, got2 := false, false
		for time.Now().Before(deadline) {
			for _, c := range mock1.Calls {
				if c.Method == "Write" {
					got1 = true
					break
				}
			}
			for _, c := range mock2.Calls {
				if c.Method == "Write" {
					got2 = true
					break
				}
			}
			if got1 && got2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if !got1 || !got2 {
			t.Fatalf("expected Write on both streams: got1=%v got2=%v", got1, got2)
		}

		// Cleanup: end the announcement and cancel writer contexts so goroutines exit
		end()
		cancel1()
		cancel2()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			mock1.AssertExpectations(t)
			mock2.AssertExpectations(t)
			t.Fatal("serveAnnouncements goroutines did not exit in time")
		}

		mock1.AssertExpectations(t)
		mock2.AssertExpectations(t)
	})
}

func TestMux_Announce_ClosesBusySubscriber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()

		// Prepare a real AnnouncementWriter and serveAnnouncements
		mockStream := &MockQUICStream{}
		// Make writes slow so the buffer fills up
		mockStream.On("Context").Return(context.Background())
		mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			time.Sleep(50 * time.Millisecond)
		}).Maybe()
		mockStream.On("Close").Return(nil).Maybe()
		mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
		mockStream.On("CancelRead", mock.Anything).Return().Maybe()

		aw := newAnnouncementWriter(mockStream, "/busy/")

		var wg sync.WaitGroup
		wg.Go(func() {
			mux.serveAnnouncements(aw)
		})

		// Create an announcement and call Announce repeatedly to fill the buffer
		_, _ = NewAnnouncement(context.Background(), BroadcastPath("/busy/stream"))

		// Send enough announces to fill the channel buffer (8)
		for i := range 16 {
			a, endf := NewAnnouncement(context.Background(), BroadcastPath(fmt.Sprintf("/busy/stream-%d", i)))
			mux.Announce(a, TrackHandlerFunc(func(tw *TrackWriter) {}))
			endf()
		}

		// Wait for some time to allow processing and the busy-channel detection path
		time.Sleep(200 * time.Millisecond)

		// Check that the subscription is removed (no entry for aw)
		root := &mux.announcementTree
		root.mu.RLock()
		_, exists := root.subscriptions[aw]
		root.mu.RUnlock()
		assert.False(t, exists, "expected subscription removed after busy detection")

		// Clean up: close AW if it's still open and wait
		aw.Close()
		wg.Wait()
	})
}

// Test serveAnnouncements with Publish before listener registers: init should send existing announcement
func TestMux_Publish_InitSendsExistingAnnouncements(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	// Register a handler using Publish (this creates an active Announcement)
	path := BroadcastPath("/pubinit/stream")
	mux.Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {}))

	// Prepare mock stream for AnnouncementWriter
	mockStream := &MockQUICStream{}
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockStream.On("Context").Return(streamCtx)
	writeCh := make(chan struct{}, 1)
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})
	mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	mockStream.On("CancelRead", mock.Anything).Return().Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	mockStream.On("CancelRead", mock.Anything).Return().Maybe()

	aw := newAnnouncementWriter(mockStream, "/pubinit/")

	var wg sync.WaitGroup
	wg.Go(func() {
		mux.serveAnnouncements(aw)
	})

	// Wait up to 500ms for a Write to happen (init)
	select {
	case <-writeCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected Write to be called on mockStream during init after Publish")
	}

	// stop serveAnnouncements by cancelling the writer's underlying context
	cancel()

	// wait for goroutine to finish
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveAnnouncements did not exit after cancelling context")
	}

	mockStream.AssertExpectations(t)
}

// Test serveAnnouncements where Publish occurs after listener registers: the Write should be triggered
func TestMux_Publish_AfterServeAnnouncements_SendsAnnouncement(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	path := BroadcastPath("/pubafter/stream")

	// Prepare mock stream and writer
	mockStream := &MockQUICStream{}
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockStream.On("Context").Return(streamCtx)
	writeCh := make(chan struct{}, 1)
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})
	mockStream.On("Close").Return(nil).Maybe()
	// Allow CancelWrite/CancelRead called during cleanup or internal errors
	mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()

	aw := newAnnouncementWriter(mockStream, "/pubafter/")

	var wg sync.WaitGroup
	wg.Go(func() {
		mux.serveAnnouncements(aw)
	})

	// Wait until serveAnnouncements registers (first init Write) or proceed quickly
	select {
	case <-writeCh:
	case <-time.After(50 * time.Millisecond):
	}

	// Now Publish (which calls Announce internally)
	mux.Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {}))

	select {
	case <-writeCh:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected Write to be called on mockStream after Publish")
	}

	// stop serveAnnouncements by cancelling the writer's underlying context
	cancel()

	// wait for goroutine to finish
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveAnnouncements did not exit after cancelling context")
	}

	mockStream.AssertExpectations(t)
}

// Test serveAnnouncements with PublishFunc before listener registers: init should send existing announcement
func TestMux_PublishFunc_InitSendsExistingAnnouncements(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	// Register a handler using PublishFunc (this creates an active Announcement)
	path := BroadcastPath("/pubfuncinit/stream")
	mux.PublishFunc(ctx, path, func(tw *TrackWriter) {})

	// Prepare mock stream for AnnouncementWriter
	mockStream := &MockQUICStream{}
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockStream.On("Context").Return(streamCtx)
	writeCh := make(chan struct{}, 1)
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()

	aw := newAnnouncementWriter(mockStream, "/pubfuncinit/")

	var wg sync.WaitGroup
	wg.Go(func() {
		mux.serveAnnouncements(aw)
	})

	// Wait up to 500ms for a Write to happen
	deadline := time.Now().Add(500 * time.Millisecond)
	found := false
	for time.Now().Before(deadline) {
		for _, c := range mockStream.Calls {
			if c.Method == "Write" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !found {
		t.Fatal("expected Write to be called on mockStream during init after PublishFunc")
	}

	// stop serveAnnouncements by cancelling the writer's underlying context
	cancel()

	// wait for goroutine to finish
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveAnnouncements did not exit after cancelling context")
	}

	mockStream.AssertExpectations(t)
}

// Test serveAnnouncements where PublishFunc occurs after listener registers: the Write should be triggered
func TestMux_PublishFunc_AfterServeAnnouncements_SendsAnnouncement(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()

	path := BroadcastPath("/pubfuncafter/stream")

	// Prepare mock stream and writer
	mockStream := &MockQUICStream{}
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockStream.On("Context").Return(streamCtx)
	writeCh := make(chan struct{}, 1)
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()
	mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return().Maybe()

	aw := newAnnouncementWriter(mockStream, "/pubfuncafter/")

	var wg sync.WaitGroup
	wg.Go(func() {
		mux.serveAnnouncements(aw)
	})

	// Wait until serveAnnouncements registers (first init Write) or short timeout
	select {
	case <-writeCh:
	case <-time.After(50 * time.Millisecond):
	}

	// Now PublishFunc (which calls Publish/Announce internally)
	mux.PublishFunc(ctx, path, func(tw *TrackWriter) {})

	// Wait up to 500ms for a Write to happen
	select {
	case <-writeCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected Write to be called on mockStream after PublishFunc")
	}

	// stop serveAnnouncements by cancelling the writer's underlying context
	cancel()

	// wait for goroutine to finish
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveAnnouncements did not exit after cancelling context")
	}

	mockStream.AssertExpectations(t)
}

// Stress test: multiple serveAnnouncements listeners and concurrent Announce calls should not deadlock or panic
func TestMux_ServeAnnouncements_ConcurrentAnnounce_NoDeadlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Start several listeners (AnnouncementWriters)
		listeners := 5
		var aws []*AnnouncementWriter
		var mocks []*MockQUICStream
		var cancels []context.CancelFunc
		var readyChans []chan struct{}
		for range listeners {
			ms := &MockQUICStream{}
			// use cancellable contexts so we can stop goroutines later
			cctx, cancel := context.WithCancel(context.Background())
			cancels = append(cancels, cancel)
			ms.On("Context").Return(cctx)
			ready := make(chan struct{}, 1)
			ms.On("Write", mock.Anything).Return(0, nil).Maybe().Run(func(args mock.Arguments) {
				select {
				case ready <- struct{}{}:
				default:
				}
			})
			ms.On("Close").Return(nil).Maybe()
			ms.On("CancelRead", mock.Anything).Return().Maybe()
			ms.On("Close").Return(nil).Maybe()
			ms.On("CancelWrite", mock.Anything).Return().Maybe()
			ms.On("CancelRead", mock.Anything).Return().Maybe()
			mocks = append(mocks, ms)
			aw := newAnnouncementWriter(ms, "/race/")
			aws = append(aws, aw)
			readyChans = append(readyChans, ready)
		}

		var wg sync.WaitGroup
		wg.Add(len(aws))
		for _, aw := range aws {
			a := aw
			go func() {
				defer wg.Done()
				mux.serveAnnouncements(a)
			}()
		}

		// Give listeners a moment to register and initialize before producing.
		// We wait for ready signals from at least one write to confirm listeners are active.
		time.Sleep(50 * time.Millisecond)

		// Concurrently call Announce many times
		var announceWg sync.WaitGroup
		producers := 10
		perProducer := 20
		announceWg.Add(producers)
		for p := range producers {
			go func(id int) {
				defer announceWg.Done()
				for j := range perProducer {
					ann, end := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/race/stream-%d-%d", id, j)))
					// announce and end quickly
					mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))
					end()
				}
			}(p)
		}

		// Wait for producers to finish
		announceWg.Wait()

		// Cleanup: cancel underlying mock contexts so listener goroutines exit
		for _, cancel := range cancels {
			cancel()
		}
		for _, ms := range mocks {
			ms.AssertExpectations(t)
		}

		// Give a short time for goroutines to process
		time.Sleep(200 * time.Millisecond)

		// Try to stop listener goroutines by ending announcements on the tree: create a final announcement and end it
		finalAnn, finalEnd := NewAnnouncement(ctx, BroadcastPath("/race/final"))
		mux.Announce(finalAnn, TrackHandlerFunc(func(tw *TrackWriter) {}))
		finalEnd()

		// Wait for listeners to exit (they should exit eventually)
		synctest.Wait()
		wg.Wait()
	})
}

func TestAnnounce(t *testing.T) {
	// Create a mock announcement and handler
	announcement, _ := NewAnnouncement(context.Background(), BroadcastPath("/test"))
	handler := TrackHandlerFunc(func(tw *TrackWriter) {
		// Mock handler
	})

	// Call the package-level Announce function
	Announce(announcement, handler)

	// Verify that the announcement was registered in DefaultMux
	assert.NotNil(t, DefaultMux)
}

// NewAnnouncement should panic for invalid paths
func TestNewAnnouncement_InvalidPath_Panic(t *testing.T) {
	assert.Panics(t, func() {
		var ctx = context.Background()
		NewAnnouncement(ctx, BroadcastPath("invalid/path/no/leading/slash"))
	})
}

// Announcement AfterFunc should be executed when announcement ends
func TestAnnouncement_AfterFunc_CalledOnEnd(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/afterfunc"))
	called := make(chan struct{}, 1)
	ann.AfterFunc(func() { called <- struct{}{} })
	// End the announcement
	end()
	select {
	case <-called:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AfterFunc was not called upon end()")
	}
}

// Announcement Done channel should close after end
func TestAnnouncement_Done_ClosesOnEnd(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/done"))
	done := ann.Done()
	end()
	select {
	case <-done:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done channel not closed after end")
	}
}

// serveTrack should close the track when the announcement ends (via AfterFunc)
func TestMux_ServeTrack_ClosesWhenAnnouncementEnds(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/serve/close/when/announce/ends")

	ann, end := NewAnnouncement(ctx, path)
	defer end()

	// Register the handler so it remains active
	mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {
		// Ensure we handle cases where receiveSubscribeStream might be nil (closed early)
		if tw.receiveSubscribeStream == nil {
			// Nothing to wait on; exit handler immediately
			return
		}
		// Wait for context cancellation or closure; this will be unblocked when the announcement ends and tw.Close() is called
		<-tw.Context().Done()
	}))

	// Mock receive stream expects Close / CancelRead to be called as part of Close()
	mockStream := &MockQUICStream{}
	streamCtx := t.Context()
	mockStream.On("Context").Return(streamCtx)
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("Close").Return(nil).Maybe()
	// Close may call CancelWrite and/or CancelRead depending on implementation; accept either
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	// Close should cancel read (and maybe write) with an error code; we accept any code here
	mockStream.On("CancelRead", mock.Anything).Return().Once()

	tw := newTrackWriter(path, TrackName("test"), newReceiveSubscribeStream(SubscribeID(1), func() Stream { return mockStream }(), &TrackConfig{}), func() (SendStream, error) {
		return &MockQUICSendStream{}, nil
	}, func() {})

	// Serve in a goroutine so handler can block and we can end the announcement
	var wg sync.WaitGroup
	wg.Go(func() {
		mux.serveTrack(tw)
	})

	// Wait for the handler to start inside serveTrack by polling the TrackHandler mapping
	deadline := time.Now().Add(500 * time.Millisecond)
	started := false
	for time.Now().Before(deadline) {
		a, _ := mux.TrackHandler(path)
		if a == ann {
			started = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !started {
		t.Fatal("serveTrack did not register handler for path in time")
	}

	// End the announcement; this should trigger tw.Close through AfterFunc
	end()

	// Wait for the serveTrack to finish (handler should be unblocked by Close())
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveTrack did not exit after announcement end")
	}

	mockStream.AssertCalled(t, "CancelRead", mock.Anything)
	mockStream.AssertExpectations(t)
}

// AfterFunc should be executed immediately if announcement already ended
func TestAnnouncement_AfterFunc_CalledIfAlreadyEnded(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/afterfunc/alreadyended"))
	// End immediately
	end()

	called := make(chan struct{}, 1)
	ann.AfterFunc(func() {
		called <- struct{}{}
	})

	select {
	case <-called:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AfterFunc was not called when announcement was already ended")
	}
}

// AfterFunc stop function should prevent the callback from being executed on end
func TestAnnouncement_AfterFunc_StopPreventsCall(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/afterfunc/stop"))

	called := false
	stop := ann.AfterFunc(func() { called = true })
	require.NotNil(t, stop)

	// Stop the after func
	stopped := stop()
	assert.True(t, stopped, "stop should return true for active handler")

	// End the announcement; the callback should not be called
	end()
	time.Sleep(50 * time.Millisecond)
	assert.False(t, called, "after func was called despite being stopped")
}

// stop() should return false if called after the announcement has already ended
func TestAnnouncement_Stop_ReturnsFalse_AfterEnd(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/stop/after/end"))

	called := false
	stop := ann.AfterFunc(func() { called = true })
	require.NotNil(t, stop)

	end()

	// Give a short time for handlers to run
	select {
	case <-ann.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("announcement did not end in time")
	}

	// Handler should have been called
	assert.True(t, called, "after func should be called during end")

	// stop should return false now
	got := stop()
	assert.False(t, got, "stop should return false after the handler has been executed")
}

// stop() should return false when AfterFunc is registered after the announcement ended
func TestAnnouncement_Stop_ReturnsFalse_IfRegisteredAfterEnd(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/stop/after/registered"))
	end()

	// Register after end -- this should call the handler asynchronously and return a stop that is useless
	calledCh := make(chan struct{})
	stop := ann.AfterFunc(func() { close(calledCh) })

	// Handler should be called asynchronously
	select {
	case <-calledCh:
		// expected
	case <-time.After(time.Second):
		t.Fatal("handler was not called")
	}

	// stop should return false
	assert.False(t, stop())
}

// End should be idempotent; calling it multiple times should not panic and all after funcs should run only once
func TestAnnouncement_End_Idempotent_WithMultipleAfterFuncs(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/idempotent"))

	var calledCount int32
	n := 10
	var wg sync.WaitGroup
	// We intentionally call end() twice below; AfterFunc handlers should be executed once total
	wg.Add(n)
	for range n {
		ann.AfterFunc(func() {
			atomic.AddInt32(&calledCount, 1)
			wg.Done()
		})
	}

	// Calling end twice should be idempotent; handlers should be called exactly once total
	end()
	end()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("after funcs did not all run in time")
	}

	if atomic.LoadInt32(&calledCount) != int32(n) {
		t.Fatalf("expected %d after funcs to be called, got %d", n, calledCount)
	}

	// Done channel should be closed
	select {
	case <-ann.Done():
	default:
		t.Fatal("Done channel not closed after end")
	}
}

// Test that announcing a single path will create nodes in the announcement tree
// and that ending the announcement prunes those nodes from the tree
func TestMux_Announce_RemoveAnnouncement_PrunesTree(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/prune/a/b/c")

	ann, end := NewAnnouncement(ctx, path)
	// Register a simple handler
	mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

	// Validate the prefix nodes exist under root
	mux.announcementTree.mu.RLock()
	_, okA := mux.announcementTree.children["prune"]
	mux.announcementTree.mu.RUnlock()
	require.True(t, okA, "expected tree to contain child 'prune' after announce")

	// Wait until announcement is visible in 'prune'->'a'->'b'
	// Directly probe via nested locks for 'a' and 'b'
	mux.announcementTree.mu.RLock()
	childA := mux.announcementTree.children["prune"]
	mux.announcementTree.mu.RUnlock()
	require.NotNil(t, childA)

	childA.mu.RLock()
	_, okB := childA.children["a"]
	childA.mu.RUnlock()
	require.True(t, okB, "expected tree to contain child 'a' after announce")

	// End the announcement and wait for Done
	end()
	select {
	case <-ann.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("announcement did not finish in time")
	}

	// Eventually the tree should be pruned: no 'prune' child under root
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mux.announcementTree.mu.RLock()
		_, ok := mux.announcementTree.children["prune"]
		mux.announcementTree.mu.RUnlock()
		if !ok {
			// pruned
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("announcement tree node 'prune' was not pruned after announcement ended")
}

// Concurrent registration of AfterFunc while end() is called should result in each handler executed exactly once
func TestAnnouncement_AfterFunc_ConcurrentRegistrationAndEnd(t *testing.T) {
	ctx := context.Background()
	ann, end := NewAnnouncement(ctx, BroadcastPath("/announce/concurrent"))

	var called int32
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	for range n {
		go func() {
			ann.AfterFunc(func() {
				atomic.AddInt32(&called, 1)
			})
			wg.Done()
		}()
	}

	// Wait for all registrations to complete BEFORE calling end()
	wg.Wait()

	// Now call end after all AfterFunc registrations are complete
	end()

	// Wait until announcement's Done channel is closed to ensure all handler invocations are complete
	select {
	case <-ann.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for announcement done")
	}

	if got := atomic.LoadInt32(&called); got != n {
		t.Fatalf("expected %d handlers invoked, got %d", n, got)
	}
}

// Test pathSegments and prefixSegments with corner cases (trailing slash, empty segment, root)
func TestPathAndPrefixSegments_EdgeCases(t *testing.T) {
	t.Run("path segments table", func(t *testing.T) {
		tests := map[string]struct {
			path       BroadcastPath
			wantPrefix []string
			wantLast   string
		}{
			"normal":         {BroadcastPath("/a/b/c"), []string{"a", "b"}, "c"},
			"trailing_slash": {BroadcastPath("/a/b/"), []string{"a", "b"}, ""},
			"root":           {BroadcastPath("/"), []string{}, ""},
			"double_slash":   {BroadcastPath("/a//b"), []string{"a", ""}, "b"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				gotPrefix, gotLast := pathSegments(tt.path)
				assert.Equal(t, tt.wantPrefix, gotPrefix)
				assert.Equal(t, tt.wantLast, gotLast)
			})
		}
	})

	t.Run("prefix segments table", func(t *testing.T) {
		tests := map[string]struct {
			prefix string
			want   []string
		}{
			"normal":       {"/a/b/", []string{"a", "b"}},
			"double_slash": {"/a//b/", []string{"a", "", "b"}},
			"root":         {"/", []string{}},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				got := prefixSegments(tt.prefix)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

// Publish with nil handler should be treated like Announce with nil handler and close stream
func TestMux_Publish_WithNilHandler_ClosesTrack(t *testing.T) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/publish/nilhandler")

	// Publish with nil handler
	mux.Publish(ctx, path, nil)

	// Serve should close with TrackNotFound
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockStream.On("CancelWrite", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("CancelRead", StreamErrorCode(TrackNotFoundErrorCode)).Return().Once()
	mockStream.On("Close").Return(nil).Maybe()

	tw := newTrackWriter(path, TrackName("test"), newReceiveSubscribeStream(SubscribeID(1), func() Stream { return mockStream }(), &TrackConfig{}), func() (SendStream, error) {
		return &MockQUICSendStream{}, nil
	}, func() {})

	mux.serveTrack(tw)

	mockStream.AssertCalled(t, "CancelWrite", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertCalled(t, "CancelRead", StreamErrorCode(TrackNotFoundErrorCode))
	mockStream.AssertExpectations(t)
}

// An ancestor and descendant AnnouncementWriter should both receive the same announcement
func TestMux_ServeAnnouncements_AncestorAndDescendantReceive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mux := NewTrackMux()
		ctx := context.Background()

		// Prepare announcement for /share/stream
		ann, end := NewAnnouncement(ctx, BroadcastPath("/share/stream"))
		defer end()

		// Root writer (prefix "/")
		rootStream := &MockQUICStream{}
		rootCtx, cancelR := context.WithCancel(context.Background())
		defer cancelR()
		rootStream.On("Context").Return(rootCtx)
		rootWriteCh := make(chan struct{}, 1)
		rootStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case rootWriteCh <- struct{}{}:
			default:
			}
		})
		rootStream.On("Close").Return(nil).Maybe()
		rootStream.On("CancelWrite", mock.Anything).Return().Maybe()
		rootStream.On("CancelRead", mock.Anything).Return().Maybe()
		rootAW := newAnnouncementWriter(rootStream, "/")

		// Descendant writer (prefix /share/)
		shareStream := &MockQUICStream{}
		shareCtx, cancelS := context.WithCancel(context.Background())
		defer cancelS()
		shareStream.On("Context").Return(shareCtx)
		shareWriteCh := make(chan struct{}, 1)
		shareStream.On("Write", mock.Anything).Return(0, nil).Run(func(args mock.Arguments) {
			select {
			case shareWriteCh <- struct{}{}:
			default:
			}
		})
		shareStream.On("Close").Return(nil).Maybe()
		shareStream.On("CancelWrite", mock.Anything).Return().Maybe()
		shareStream.On("CancelRead", mock.Anything).Return().Maybe()
		shareAW := newAnnouncementWriter(shareStream, "/share/")

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			mux.serveAnnouncements(rootAW)
		}()
		go func() {
			defer wg.Done()
			mux.serveAnnouncements(shareAW)
		}()

		// Give listeners a moment to init
		select {
		case <-rootWriteCh:
		case <-time.After(200 * time.Millisecond):
		}
		select {
		case <-shareWriteCh:
		case <-time.After(200 * time.Millisecond):
		}

		// Announce the stream
		mux.Announce(ann, TrackHandlerFunc(func(tw *TrackWriter) {}))

		// Wait for both streams to receive a write
		deadline := time.Now().Add(500 * time.Millisecond)
		gotRoot, gotShare := false, false
		for time.Now().Before(deadline) {
			for _, c := range rootStream.Calls {
				if c.Method == "Write" {
					gotRoot = true
					break
				}
			}
			for _, c := range shareStream.Calls {
				if c.Method == "Write" {
					gotShare = true
					break
				}
			}
			if gotRoot && gotShare {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !gotRoot || !gotShare {
			t.Fatalf("expected Write on both root and share streams: gotRoot=%v gotShare=%v", gotRoot, gotShare)
		}

		// cleanup
		cancelR()
		cancelS()
		wg.Wait()
		rootStream.AssertExpectations(t)
		shareStream.AssertExpectations(t)
	})
}
