package moqt

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewAnnouncement(t *testing.T) {
	tests := map[string]struct {
		path     BroadcastPath
		expected string
	}{
		"valid path": {
			path:     BroadcastPath("/test/path"),
			expected: "/test/path",
		},
		"path with spaces and symbols": {
			path:     BroadcastPath("/test/path with spaces/and-dashes_and.dots"),
			expected: "/test/path with spaces/and-dashes_and.dots",
		},
		"root path": {
			path:     BroadcastPath("/"),
			expected: "/",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			assert.NotNil(t, announcement)
			assert.NotNil(t, end)
			assert.Equal(t, tt.path, announcement.path)
			assert.True(t, announcement.IsActive())
		})
	}
}

func TestNewAnnouncement_InvalidPaths(t *testing.T) {
	invalid := map[string]BroadcastPath{
		"empty":            BroadcastPath(""),
		"no leading slash": BroadcastPath("test/path"),
		"only dots prefix": BroadcastPath("./test"),
	}

	for name, p := range invalid {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			assert.Panics(t, func() { _, _ = NewAnnouncement(ctx, p) })
		})
	}
}

func TestAnnouncement_BroadcastPath(t *testing.T) {
	tests := map[string]struct {
		path     BroadcastPath
		expected BroadcastPath
	}{
		"standard path": {
			path:     BroadcastPath("/test/path"),
			expected: BroadcastPath("/test/path"),
		},
		"root path": {
			path:     BroadcastPath("/"),
			expected: BroadcastPath("/"),
		},
		"complex path": {
			path:     BroadcastPath("/complex/path/with/multiple/segments"),
			expected: BroadcastPath("/complex/path/with/multiple/segments"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, _ := NewAnnouncement(ctx, tt.path)

			result := announcement.BroadcastPath()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAnnouncement_String(t *testing.T) {
	tests := map[string]struct {
		path           BroadcastPath
		shouldEnd      bool
		expectedActive string
		expectedEnded  string
	}{
		"standard path": {
			path:           BroadcastPath("/test/path"),
			shouldEnd:      true,
			expectedActive: "{ announce_status: active, broadcast_path: /test/path }",
			expectedEnded:  "{ announce_status: ended, broadcast_path: /test/path }",
		},
		"root path": {
			path:           BroadcastPath("/"),
			shouldEnd:      true,
			expectedActive: "{ announce_status: active, broadcast_path: / }",
			expectedEnded:  "{ announce_status: ended, broadcast_path: / }",
		},
		"special characters": {
			path:           BroadcastPath("/test/path with spaces/and-dashes_and.dots"),
			shouldEnd:      false,
			expectedActive: "{ announce_status: active, broadcast_path: /test/path with spaces/and-dashes_and.dots }",
			expectedEnded:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			result := announcement.String()
			assert.Equal(t, tt.expectedActive, result)

			if tt.shouldEnd {
				end()
				result = announcement.String()
				assert.Equal(t, tt.expectedEnded, result)
			}
		})
	}
}

func TestAnnouncement_IsActive(t *testing.T) {
	tests := map[string]struct {
		path              BroadcastPath
		shouldEnd         bool
		expectedBeforeEnd bool
		expectedAfterEnd  bool
	}{
		"active announcement": {
			path:              BroadcastPath("/test/path"),
			shouldEnd:         false,
			expectedBeforeEnd: true,
			expectedAfterEnd:  true,
		},
		"ended announcement": {
			path:              BroadcastPath("/test/path"),
			shouldEnd:         true,
			expectedBeforeEnd: true,
			expectedAfterEnd:  false,
		},
		"root path active": {
			path:              BroadcastPath("/"),
			shouldEnd:         false,
			expectedBeforeEnd: true,
			expectedAfterEnd:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			assert.Equal(t, tt.expectedBeforeEnd, announcement.IsActive())

			if tt.shouldEnd {
				end()
				assert.Equal(t, tt.expectedAfterEnd, announcement.IsActive())
			}
		})
	}
}

func TestAnnouncement_End(t *testing.T) {
	tests := map[string]struct {
		path             BroadcastPath
		multipleEndCalls bool
		expectedAfterEnd bool
	}{
		"single end call": {
			path:             BroadcastPath("/test/path"),
			multipleEndCalls: false,
			expectedAfterEnd: false,
		},
		"multiple end calls": {
			path:             BroadcastPath("/test/path"),
			multipleEndCalls: true,
			expectedAfterEnd: false,
		},
		"root path end": {
			path:             BroadcastPath("/"),
			multipleEndCalls: false,
			expectedAfterEnd: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			assert.True(t, announcement.IsActive())

			end()
			if tt.multipleEndCalls {
				end()
				end()
			}

			assert.Equal(t, tt.expectedAfterEnd, announcement.IsActive())
		})
	}
}

func TestAnnouncement_ContextDone(t *testing.T) {
	tests := map[string]struct {
		path        BroadcastPath
		endDelay    time.Duration
		timeout     time.Duration
		expectClose bool
	}{
		"end after delay": {
			path:        BroadcastPath("/test/path"),
			endDelay:    100 * time.Millisecond,
			timeout:     200 * time.Millisecond,
			expectClose: true,
		},
		"no end call": {
			path:        BroadcastPath("/test/path"),
			endDelay:    0,
			timeout:     50 * time.Millisecond,
			expectClose: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			// Test that Context().Done() is not closed initially
			select {
			case <-announcement.Done():
				if !tt.expectClose {
					t.Error("Expected Context().Done() channel to not be closed initially")
				}
			default:
				// This is the expected behavior for non-closed channels
			}

			if tt.endDelay > 0 {
				go func() {
					time.Sleep(tt.endDelay)
					end()
				}()

				select {
				case <-announcement.Done():
					assert.True(t, tt.expectClose, "Channel closed when not expected")
				case <-time.After(tt.timeout):
					assert.False(t, tt.expectClose, "Expected channel to be closed but timeout occurred")
				}
			}
		})
	}
}

func TestAnnouncement_WithCancelledContext(t *testing.T) {
	tests := map[string]struct {
		path              BroadcastPath
		cancelImmediately bool
		sleepDuration     time.Duration
		expectedActive    bool
	}{
		"cancel after creation": {
			path:              BroadcastPath("/test/path"),
			cancelImmediately: false,
			sleepDuration:     10 * time.Millisecond,
			expectedActive:    false,
		},
		"already cancelled": {
			path:              BroadcastPath("/test/path"),
			cancelImmediately: true,
			sleepDuration:     0,
			expectedActive:    false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if tt.cancelImmediately {
				cancel()
			}

			announcement, _ := NewAnnouncement(ctx, tt.path)

			if !tt.cancelImmediately {
				assert.True(t, announcement.IsActive())
				cancel()
				if tt.sleepDuration > 0 {
					time.Sleep(tt.sleepDuration)
				}
			}

			assert.Equal(t, tt.expectedActive, announcement.IsActive())
		})
	}
}

func TestAnnouncement_ConcurrentContextDone(t *testing.T) {
	tests := map[string]struct {
		path          BroadcastPath
		numGoroutines int
		endDelay      time.Duration
		timeout       time.Duration
	}{
		"concurrent context done": {
			path:          BroadcastPath("/test/path"),
			numGoroutines: 10,
			endDelay:      50 * time.Millisecond,
			timeout:       500 * time.Millisecond,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			announcement, end := NewAnnouncement(ctx, tt.path)

			results := make(chan bool, tt.numGoroutines)

			for i := 0; i < tt.numGoroutines; i++ {
				go func() {
					select {
					case <-announcement.Done():
						results <- true
					case <-time.After(tt.timeout):
						results <- false
					}
				}()
			}

			time.Sleep(tt.endDelay)
			end()

			for i := 0; i < tt.numGoroutines; i++ {
				select {
				case result := <-results:
					assert.True(t, result, "Expected all goroutines to receive end signal")
				case <-time.After(200 * time.Millisecond):
					t.Error("Timeout waiting for goroutine to complete")
				}
			}
		})
	}
}

// TestAnnouncement_PathCost verifies the microsecond storage converts to a
// time.Duration and defaults to zero for fresh announcements.
func TestAnnouncement_PathCost(t *testing.T) {
	ann, end := NewAnnouncement(context.Background(), BroadcastPath("/test/path"))
	defer end()
	assert.Equal(t, time.Duration(0), ann.PathCost(), "fresh announcement has no accumulated cost")

	ann.pathCost = 150_000
	assert.Equal(t, 150*time.Millisecond, ann.PathCost())
}
