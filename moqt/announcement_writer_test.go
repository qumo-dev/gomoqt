package moqt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAnnouncementWriter creates a test AnnouncementWriter with default setup.
// It uses the default prefix "/test/" unless a custom prefix is provided.
// Optional setup functions configure the underlying FakeQUICStream.
func newTestAnnouncementWriter(t *testing.T, opts ...func(*FakeQUICStream)) *AnnouncementWriter {
	mockStream := &FakeQUICStream{}
	for _, f := range opts {
		if f != nil {
			f(mockStream)
		}
	}
	return newAnnouncementWriter(mockStream, "/test/", nil)
}

func TestNewAnnouncementWriter(t *testing.T) {
	mockStream := &FakeQUICStream{}
	prefix := "/test/"
	logger := &slog.Logger{}
	aw := newAnnouncementWriter(mockStream, prefix, logger)

	require.NotNil(t, aw)
	assert.Equal(t, "/test/", aw.prefix)
	assert.Equal(t, mockStream, aw.stream)
	assert.Equal(t, logger, aw.logger)
	assert.NotNil(t, aw.actives)
	assert.NotNil(t, aw.ctx)

}

func TestAnnouncementWriter_Init(t *testing.T) {
	tests := map[string]struct {
		initialAnnouncements func(ctx context.Context) map[*Announcement]struct{}
		expectError          bool
		expectedActives      int
		expectedSuffixes     []string
		setupMocks           func(*FakeQUICStream)
	}{
		"empty initialization": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				return make(map[*Announcement]struct{})
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
		},
		"single active announcement": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  1,
			expectedSuffixes: []string{"stream1"},
		},
		"multiple active announcements": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
				ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream2"))
				return map[*Announcement]struct{}{ann1: {}, ann2: {}}
			},
			expectError:      false,
			expectedActives:  2,
			expectedSuffixes: []string{"stream1", "stream2"},
		},
		"inactive announcement": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, end := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
				end() // Make it inactive
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
		},
		"write error": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:     true,
			expectedActives: 0,
			setupMocks: func(mockStream *FakeQUICStream) {
				mockStream.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }
			},
		},
		"invalid path announcement": {
			initialAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/other/stream1")) // Different prefix
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			aw := newTestAnnouncementWriter(t, tt.setupMocks)
			ctx := t.Context()
			announcements := tt.initialAnnouncements(ctx)

			err := aw.init(announcements)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, aw.actives, tt.expectedActives)

				for _, suffix := range tt.expectedSuffixes {
					assert.Contains(t, aw.actives, suffix)
				}
			}

			// Verify initDone is closed after init completes (including error paths)
			select {
			case <-aw.initDone:
				// Channel is closed, this is expected
			default:
				t.Error("initDone should be closed")
			}
		})
	}
}

func TestAnnouncementWriter_SendAnnouncement_AfterInitError(t *testing.T) {
	aw := newTestAnnouncementWriter(t, func(m *FakeQUICStream) {
		m.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }
	})
	ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

	// Force init to fail
	err := aw.init(map[*Announcement]struct{}{ann: {}})
	require.Error(t, err)

	// SendAnnouncement must return promptly with init error (must not block)
	errCh := make(chan error, 1)
	go func() {
		errCh <- aw.SendAnnouncement(ann)
	}()

	select {
	case gotErr := <-errCh:
		require.Error(t, gotErr)
		assert.ErrorContains(t, gotErr, "write error")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SendAnnouncement blocked after init error")
	}

}

func TestAnnouncementWriter_Init_OnlyOnce(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

	// Call init multiple times
	err1 := aw.init(map[*Announcement]struct{}{ann: {}})
	err2 := aw.init(map[*Announcement]struct{}{}) // Second call should be ignored

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, aw.actives, 1)
	assert.Contains(t, aw.actives, "stream1")

}

func TestAnnouncementWriter_Init_StreamError(t *testing.T) {
	streamError := &transport.StreamError{
		StreamID:  transport.StreamID(123),
		ErrorCode: transport.StreamErrorCode(42),
	}

	aw := newTestAnnouncementWriter(t, func(m *FakeQUICStream) {
		m.WriteFunc = func(p []byte) (int, error) { return 0, streamError }
	})

	ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	err := aw.init(map[*Announcement]struct{}{ann: {}})

	require.Error(t, err)
	var announceErr *AnnounceError
	assert.ErrorAs(t, err, &announceErr)
	assert.Equal(t, streamError, announceErr.StreamError)

}

func TestAnnouncementWriter_Init_DuplicateAnnouncements(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1")) // Same suffix - should replace first

	err := aw.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})

	assert.NoError(t, err)
	assert.Len(t, aw.actives, 1)
	assert.Contains(t, aw.actives, "stream1")

	// Due to map iteration order the final active announcement may be either
	// ann1 or ann2. Both should remain active since init doesn't end announcements.
	active := aw.actives["stream1"]
	switch active.announcement {
	case ann1:
		assert.True(t, ann1.IsActive())
		assert.True(t, ann2.IsActive())
	case ann2:
		assert.True(t, ann2.IsActive())
		assert.True(t, ann1.IsActive())
	default:
		t.Fatalf("unexpected active announcement: %v", active)
	}

}

func TestAnnouncementWriter_Init_MultipleDifferentAnnouncements(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	// Create two announcements with different paths
	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream2"))

	err := aw.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})

	assert.NoError(t, err)
	assert.Len(t, aw.actives, 2)
	assert.Contains(t, aw.actives, "stream1")
	assert.Contains(t, aw.actives, "stream2")
	assert.Equal(t, ann1, aw.actives["stream1"].announcement)
	assert.Equal(t, ann2, aw.actives["stream2"].announcement)

}

func TestAnnouncementWriter_Init_DeadlockIssue(t *testing.T) {
	// This test verifies that init() with duplicate announcements doesn't cause deadlock
	// after the implementation was fixed to use goroutines in OnEnd callbacks.

	aw := newTestAnnouncementWriter(t)

	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1")) // Same suffix - should replace first

	// This should not deadlock anymore
	err := aw.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})
	assert.NoError(t, err)

	// Wait for background processing of OnEnd callbacks to complete
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n == 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n != 1 {
			t.Fatalf("timeout waiting for %d active announcements", 1)
		}
	}

	assert.Len(t, aw.actives, 1)
	assert.Contains(t, aw.actives, "stream1")

	// Both announcements should remain active since init doesn't end announcements
	activeAnn := aw.actives["stream1"]
	assert.NotNil(t, activeAnn)

	switch activeAnn.announcement {
	case ann1:
		assert.True(t, ann1.IsActive())
		assert.True(t, ann2.IsActive())
	case ann2:
		assert.True(t, ann1.IsActive())
		assert.True(t, ann2.IsActive())
	default:
		t.Fatalf("unexpected announcement: %v", activeAnn)
	}

}

func TestAnnouncementWriter_SendAnnouncement(t *testing.T) {
	tests := map[string]struct {
		broadcastPath  string
		expectError    bool
		shouldBeActive bool
	}{
		"valid path": {
			broadcastPath:  "/test/stream1",
			expectError:    false,
			shouldBeActive: true,
		},
		"invalid path": {
			broadcastPath:  "/other/stream1",
			expectError:    true,
			shouldBeActive: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			aw := newTestAnnouncementWriter(t)
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath(tt.broadcastPath))

			// Initialize the AnnouncementWriter first
			err := aw.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = aw.SendAnnouncement(ann)

			if tt.expectError {
				assert.Error(t, err)
				assert.Len(t, aw.actives, 0)
			} else {
				assert.NoError(t, err)
				assert.Len(t, aw.actives, 1)
			}

		})
	}
}

func TestAnnouncementWriter_SendAnnouncement_EncodesHops(t *testing.T) {
	var buf bytes.Buffer

	aw := newTestAnnouncementWriter(t, func(m *FakeQUICStream) {
		m.WriteFunc = buf.Write
	})
	ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err = aw.SendAnnouncement(ann)
	require.NoError(t, err)

	var decoded message.AnnounceMessage
	require.NoError(t, decoded.Decode(&buf))
	assert.Equal(t, message.ACTIVE, decoded.AnnounceStatus)
	assert.Equal(t, "stream1", decoded.BroadcastPathSuffix)
	assert.Equal(t, uint64(1), decoded.Hops)

}

func TestAnnouncementWriter_SendAnnouncement_WriteError(t *testing.T) {
	tests := map[string]struct {
		writeError   error
		expectAnnErr bool
	}{
		"stream error": {
			writeError: &transport.StreamError{
				StreamID:  transport.StreamID(123),
				ErrorCode: transport.StreamErrorCode(42),
			},
			expectAnnErr: true,
		},
		"generic error": {
			writeError:   errors.New("generic write error"),
			expectAnnErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			aw := newTestAnnouncementWriter(t, func(m *FakeQUICStream) {
				m.WriteFunc = func(p []byte) (int, error) { return 0, tt.writeError }
			})
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

			err := aw.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = aw.SendAnnouncement(ann)

			assert.Error(t, err)

			if tt.expectAnnErr {
				var announceErr *AnnounceError
				assert.ErrorAs(t, err, &announceErr)
			}

		})
	}
}

func TestAnnouncementWriter_Close(t *testing.T) {
	t.Run("closes with no active announcements", func(t *testing.T) {
		aw := newTestAnnouncementWriter(t)

		err := aw.Close()

		assert.NoError(t, err)
		assert.Nil(t, aw.actives)
		assert.NotNil(t, aw.initDone)

	})

	t.Run("closes with active announcements", func(t *testing.T) {
		aw := newTestAnnouncementWriter(t)

		// Initialize the AnnouncementWriter first
		err := aw.init(map[*Announcement]struct{}{})
		require.NoError(t, err)

		// Add an active announcement
		ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/active"))
		err = aw.SendAnnouncement(ann)
		require.NoError(t, err)

		// Verify we have an active announcement
		assert.NotNil(t, aw.actives)
		assert.Len(t, aw.actives, 1)
		assert.True(t, ann.IsActive(), "announcement should be active initially")

		err = aw.Close()

		assert.NoError(t, err)
		assert.Nil(t, aw.actives)
		assert.True(t, ann.IsActive(), "announcement should remain active after Close (AnnouncementWriter doesn't end announcements)")

	})

	t.Run("handles stream close error", func(t *testing.T) {
		expectedErr := fmt.Errorf("stream close error")
		aw := newTestAnnouncementWriter(t, func(m *FakeQUICStream) {
			m.CloseFunc = func() error { return expectedErr }
		})

		err := aw.Close()

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, aw.actives)
		assert.NotNil(t, aw.initDone)

	})
}

func TestAnnouncementWriter_CloseWithError(t *testing.T) {
	t.Run("closes with error and no active announcements", func(t *testing.T) {
		tests := map[string]struct {
			errorCode AnnounceErrorCode
		}{
			"internal error": {
				errorCode: AnnounceErrorCodeInternal,
			},
			"duplicated announce error": {
				errorCode: AnnounceErrorCodeDuplicated,
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				aw := newTestAnnouncementWriter(t)

				err := aw.CloseWithError(tt.errorCode)

				assert.NoError(t, err)
				assert.Nil(t, aw.actives)
				assert.NotNil(t, aw.initDone)

			})
		}
	})

	t.Run("closes with error and active announcements", func(t *testing.T) {
		aw := newTestAnnouncementWriter(t)

		// Initialize the AnnouncementWriter first
		err := aw.init(map[*Announcement]struct{}{})
		require.NoError(t, err)

		// Add an active announcement
		ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/active"))
		err = aw.SendAnnouncement(ann)
		require.NoError(t, err)

		// Verify we have an active announcement
		assert.NotNil(t, aw.actives)
		assert.Len(t, aw.actives, 1)
		assert.True(t, ann.IsActive(), "announcement should be active initially")

		err = aw.CloseWithError(AnnounceErrorCodeInternal)

		assert.NoError(t, err)
		assert.Nil(t, aw.actives)

	})
}

func TestAnnouncementWriter_SendAnnouncement_MultipleAnnouncements(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream2"))

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := aw.SendAnnouncement(ann1)
	err2 := aw.SendAnnouncement(ann2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, aw.actives, 2)
	assert.Contains(t, aw.actives, "stream1")
	assert.Contains(t, aw.actives, "stream2")
	assert.Equal(t, ann1, aw.actives["stream1"].announcement)
	assert.Equal(t, ann2, aw.actives["stream2"].announcement)

}

func TestAnnouncementWriter_SendAnnouncement_ReplaceExisting(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1")) // Same suffix

	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := aw.SendAnnouncement(ann1)
	assert.NoError(t, err1)

	// Send second announcement with same path - should replace the first
	err2 := aw.SendAnnouncement(ann2)
	assert.NoError(t, err2)

	// Should have only one active announcement (the newer one)
	assert.Len(t, aw.actives, 1)
	assert.Contains(t, aw.actives, "stream1")
	assert.Equal(t, ann2, aw.actives["stream1"].announcement)

	// First announcement should remain active (AnnouncementWriter doesn't end announcements)
	assert.True(t, ann1.IsActive())
	assert.True(t, ann2.IsActive())

}

func TestAnnouncementWriter_SendAnnouncement_SameInstance(t *testing.T) {
	aw := newTestAnnouncementWriter(t)
	ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := aw.SendAnnouncement(ann)
	err2 := aw.SendAnnouncement(ann)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, aw.actives, 1)
	assert.True(t, ann.IsActive())

}

func TestAnnouncementWriter_AnnouncementEnd_BackgroundProcessing(t *testing.T) {
	aw := newTestAnnouncementWriter(t)
	ann, end := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err = aw.SendAnnouncement(ann)
	assert.NoError(t, err)
	assert.Len(t, aw.actives, 1)

	end()

	// Wait for background goroutine to process and remove the active announcement
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n == 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n != 0 {
			t.Fatalf("timeout waiting for %d active announcements", 0)
		}
	}

	assert.Len(t, aw.actives, 0)

}

func TestAnnouncementWriter_BoundaryValues(t *testing.T) {
	tests := map[string]struct {
		prefix        string
		broadcastPath string
		expectError   bool
	}{
		"root prefix": {
			prefix:        "/",
			broadcastPath: "/stream1",
			expectError:   false,
		},
		"matching prefix path": {
			prefix:        "/test/",
			broadcastPath: "/test/",
			expectError:   false,
		},
		"different root": {
			prefix:        "/test/",
			broadcastPath: "/other/stream1",
			expectError:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &FakeQUICStream{}
			aw := newAnnouncementWriter(mockStream, tt.prefix, nil)
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath(tt.broadcastPath))

			// Initialize the AnnouncementWriter first
			err := aw.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = aw.SendAnnouncement(ann)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

		})
	}
}

func TestAnnouncementWriter_Performance_LargeNumberOfAnnouncements(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	const numAnnouncements = 100 // Reduced for test efficiency

	start := time.Now()
	for i := range numAnnouncements {
		ann, _ := NewAnnouncement(context.Background(), BroadcastPath(fmt.Sprintf("/test/stream%d", i)))
		err := aw.SendAnnouncement(ann)
		assert.NoError(t, err)
	}
	duration := time.Since(start)

	t.Logf("Time to send %d announcements: %v", numAnnouncements, duration)
	assert.Len(t, aw.actives, numAnnouncements)

}

func TestAnnouncementWriter_CleanupResourceLeaks(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Create and end many announcements to test cleanup
	for i := range 10 {
		ann, end := NewAnnouncement(context.Background(), BroadcastPath(fmt.Sprintf("/test/stream%d", i)))
		err := aw.SendAnnouncement(ann)
		assert.NoError(t, err)
		end()
	}

	// Wait for cleanup to finish
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n == 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n != 0 {
			t.Fatalf("timeout waiting for %d active announcements", 0)
		}
	}
	assert.Len(t, aw.actives, 0)

}

func TestAnnouncementWriter_PartialCleanup(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Create multiple announcements
	ann1, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
	ann2, end2 := NewAnnouncement(context.Background(), BroadcastPath("/test/stream2"))
	ann3, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream3"))

	assert.NoError(t, aw.SendAnnouncement(ann1))
	assert.NoError(t, aw.SendAnnouncement(ann2))
	assert.NoError(t, aw.SendAnnouncement(ann3))

	// End only some announcements
	end2()

	// Wait for background processing to handle ended announcements
	// Inline: wait until aw.actives no longer contains "stream2"
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			_, ok := aw.actives["stream2"]
			aw.mu.RUnlock()
			if !ok {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		if _, ok := aw.actives["stream2"]; ok {
			aw.mu.RUnlock()
			t.Fatalf("timeout waiting for active announcements to not contain %s", "stream2")
		}
		aw.mu.RUnlock()
	}

	// Wait for background processing to reach exactly 2 active announcements
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n == 2 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n != 2 {
			t.Fatalf("timeout waiting for %d active announcements", 2)
		}
	}
	assert.Contains(t, aw.actives, "stream1")
	assert.NotContains(t, aw.actives, "stream2")
	assert.Contains(t, aw.actives, "stream3")

}

func TestAnnouncementWriter_ConcurrentAccess(t *testing.T) {
	// NOTE: This test may occasionally cause deadlocks in the current implementation
	// when multiple goroutines compete for the same suffix, as the implementation
	// can deadlock between mutex acquisition and OnEnd callback processing.
	// The implementation should be fixed to avoid holding the mutex while calling End().

	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Test concurrent access to DIFFERENT suffixes to avoid deadlock
	done := make(chan bool, 2)
	errors := make(chan error, 2)

	go func() {
		defer func() { done <- true }()
		for i := range 5 {
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath(fmt.Sprintf("/test/stream_a_%d", i)))
			if err := aw.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer func() { done <- true }()
		for i := range 5 {
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath(fmt.Sprintf("/test/stream_b_%d", i)))
			if err := aw.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	<-done
	<-done

	close(errors)
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	assert.Len(t, aw.actives, 10) // Should have 10 different streams

}

func TestAnnouncementWriter_ConcurrentAccess_SameSuffix_DeadlockRisk(t *testing.T) {
	// This test verifies that concurrent access to the same suffix doesn't cause deadlock
	// after the implementation was fixed to use goroutines in OnEnd callbacks.

	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Test concurrent access to the SAME suffix - this should no longer cause deadlock
	done := make(chan bool, 2)
	errors := make(chan error, 2)

	go func() {
		defer func() { done <- true }()
		for range 10 {
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
			if err := aw.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer func() { done <- true }()
		for range 10 {
			ann, _ := NewAnnouncement(context.Background(), BroadcastPath("/test/stream1"))
			if err := aw.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	<-done
	<-done

	// Wait for background processing to converge
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n == 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n != 1 {
			t.Fatalf("timeout waiting for %d active announcements", 1)
		}
	}

	close(errors)
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	assert.Len(t, aw.actives, 1)
	assert.Contains(t, aw.actives, "stream1")

}

func TestAnnouncementWriter_MultipleClose(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	err1 := aw.Close()
	assert.NoError(t, err1)
	assert.Nil(t, aw.actives)

	err2 := aw.Close()
	assert.NoError(t, err2)

}

func TestAnnouncementWriter_Context(t *testing.T) {
	aw := newTestAnnouncementWriter(t)

	assert.NotNil(t, aw.Context())
	assert.NoError(t, aw.Context().Err())

}

func TestAnnouncementWriter_StressTest_HeavyConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	aw := newTestAnnouncementWriter(t)

	// Initialize the AnnouncementWriter first
	err := aw.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	const numGoroutines = 50
	const numOperationsPerGoroutine = 20
	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines*numOperationsPerGoroutine)

	// Launch multiple goroutines that compete for the same suffix aggressively
	for g := range numGoroutines {
		go func(goroutineID int) {
			defer func() { done <- true }()
			for i := range numOperationsPerGoroutine {
				// Half of them use the same suffix, half use different suffixes
				var suffixPath string
				if i%2 == 0 {
					suffixPath = "/test/contested_stream" // Same suffix - high contention
				} else {
					suffixPath = fmt.Sprintf("/test/stream_%d_%d", goroutineID, i)
				}

				ann, end := NewAnnouncement(context.Background(), BroadcastPath(suffixPath))
				if err := aw.SendAnnouncement(ann); err != nil {
					errors <- err
					return
				}
				// Randomly end some announcements to trigger OnEnd callbacks
				if i%3 == 0 {
					end()
				}
			}
		}(g)
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Wait for background processing to start removing finished announcements
	// Inline: wait until aw has at least 1 active announcement
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			aw.mu.RLock()
			n := 0
			if aw.actives != nil {
				n = len(aw.actives)
			}
			aw.mu.RUnlock()
			if n >= 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		aw.mu.RLock()
		n := 0
		if aw.actives != nil {
			n = len(aw.actives)
		}
		aw.mu.RUnlock()
		if n < 1 {
			t.Fatalf("timeout waiting for at least %d active announcements", 1)
		}
	}

	close(errors)
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify that we still have some active announcements
	assert.True(t, len(aw.actives) > 0, "Should have some active announcements remaining")

}
