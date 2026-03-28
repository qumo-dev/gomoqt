package moqt

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewAnnouncementWriter(t *testing.T) {
	mockStream := &MockQUICStream{}
	prefix := "/test/"
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)

	sas := newAnnouncementWriter(mockStream, prefix)

	require.NotNil(t, sas)
	assert.Equal(t, prefix, sas.prefix)
	assert.Equal(t, mockStream, sas.stream)
	assert.NotNil(t, sas.actives)
	assert.NotNil(t, sas.ctx)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Init(t *testing.T) {
	tests := map[string]struct {
		setupAnnouncements func(ctx context.Context) map[*Announcement]struct{}
		expectError        bool
		expectedActives    int
		expectedSuffixes   []string
		setupMocks         func(*MockQUICStream)
	}{
		"empty initialization": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				return make(map[*Announcement]struct{})
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, nil).Once()
			},
		},
		"single active announcement": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  1,
			expectedSuffixes: []string{"stream1"},
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, nil).Once()
			},
		},
		"multiple active announcements": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
				ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))
				return map[*Announcement]struct{}{ann1: {}, ann2: {}}
			},
			expectError:      false,
			expectedActives:  2,
			expectedSuffixes: []string{"stream1", "stream2"},
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, nil).Once()
			},
		},
		"inactive announcement": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, end := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
				end() // Make it inactive
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, nil).Once()
			},
		},
		"write error": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				return make(map[*Announcement]struct{})
			},
			expectError:     true,
			expectedActives: 0,
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, errors.New("write error")).Once()
			},
		},
		"invalid path announcement": {
			setupAnnouncements: func(ctx context.Context) map[*Announcement]struct{} {
				ann, _ := NewAnnouncement(ctx, BroadcastPath("/other/stream1")) // Different prefix
				return map[*Announcement]struct{}{ann: {}}
			},
			expectError:      false,
			expectedActives:  0,
			expectedSuffixes: []string{},
			setupMocks: func(mockStream *MockQUICStream) {
				ctx := context.Background()
				mockStream.On("Context").Return(ctx)
				mockStream.On("Write", mock.Anything).Return(0, nil).Once()
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &MockQUICStream{}
			ctx := context.Background()

			tt.setupMocks(mockStream)

			sas := newAnnouncementWriter(mockStream, "/test/")
			announcements := tt.setupAnnouncements(ctx)

			err := sas.init(announcements)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, sas.actives, tt.expectedActives)

				for _, suffix := range tt.expectedSuffixes {
					assert.Contains(t, sas.actives, suffix)
				}

				// Verify initCh is closed after successful initialization
				select {
				case <-sas.initCh:
					// Channel is closed, this is expected
				default:
					t.Error("initCh should be closed")
				}
			}

			mockStream.AssertExpectations(t)
		})
	}
}

func TestAnnouncementWriter_Init_OnlyOnce(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil).Once() // Should only be called once

	sas := newAnnouncementWriter(mockStream, "/test/")

	ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))

	// Call init multiple times
	err1 := sas.init(map[*Announcement]struct{}{ann: {}})
	err2 := sas.init(map[*Announcement]struct{}{}) // Second call should be ignored

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, sas.actives, 1)
	assert.Contains(t, sas.actives, "stream1")

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Init_StreamError(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	streamError := &StreamError{
		StreamID:  StreamID(123),
		ErrorCode: StreamErrorCode(42),
	}

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, streamError).Once()

	sas := newAnnouncementWriter(mockStream, "/test/")

	err := sas.init(map[*Announcement]struct{}{})

	require.Error(t, err)
	var announceErr *AnnounceError
	assert.ErrorAs(t, err, &announceErr)
	assert.Equal(t, streamError, announceErr.StreamError)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Init_DuplicateAnnouncements(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil) // Allow any number of Write calls

	sas := newAnnouncementWriter(mockStream, "/test/")

	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1")) // Same suffix - should replace first

	err := sas.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})

	assert.NoError(t, err)
	assert.Len(t, sas.actives, 1)
	assert.Contains(t, sas.actives, "stream1")

	// Due to map iteration order the final active announcement may be either
	// ann1 or ann2. Both should remain active since init doesn't end announcements.
	active := sas.actives["stream1"]
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

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Init_MultipleDifferentAnnouncements(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil) // Allow any number of Write calls

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Create two announcements with different paths
	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))

	err := sas.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})

	assert.NoError(t, err)
	assert.Len(t, sas.actives, 2)
	assert.Contains(t, sas.actives, "stream1")
	assert.Contains(t, sas.actives, "stream2")
	assert.Equal(t, ann1, sas.actives["stream1"].announcement)
	assert.Equal(t, ann2, sas.actives["stream2"].announcement)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Init_DeadlockIssue(t *testing.T) {
	// This test verifies that init() with duplicate announcements doesn't cause deadlock
	// after the implementation was fixed to use goroutines in OnEnd callbacks.

	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1")) // Same suffix - should replace first

	// This should not deadlock anymore
	err := sas.init(map[*Announcement]struct{}{ann1: {}, ann2: {}})
	assert.NoError(t, err)

	// Wait for background processing of OnEnd callbacks to complete
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n == 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n != 1 {
			t.Fatalf("timeout waiting for %d active announcements", 1)
		}
	}

	assert.Len(t, sas.actives, 1)
	assert.Contains(t, sas.actives, "stream1")

	// Both announcements should remain active since init doesn't end announcements
	activeAnn := sas.actives["stream1"]
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

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_SendAnnouncement(t *testing.T) {
	tests := map[string]struct {
		prefix         string
		broadcastPath  string
		expectError    bool
		shouldBeActive bool
	}{
		"valid path": {
			prefix:         "/test/",
			broadcastPath:  "/test/stream1",
			expectError:    false,
			shouldBeActive: true,
		},
		"invalid path": {
			prefix:         "/test/",
			broadcastPath:  "/other/stream1",
			expectError:    true,
			shouldBeActive: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &MockQUICStream{}
			ctx := context.Background()

			mockStream.On("Context").Return(ctx)
			if !tt.expectError {
				mockStream.On("Write", mock.Anything).Return(0, nil).Times(2) // One for init, one for SendAnnouncement
			} else {
				mockStream.On("Write", mock.Anything).Return(0, nil).Times(1) // Only for init
			}

			sas := newAnnouncementWriter(mockStream, tt.prefix)
			ann, _ := NewAnnouncement(ctx, BroadcastPath(tt.broadcastPath))

			// Initialize the AnnouncementWriter first
			err := sas.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = sas.SendAnnouncement(ann)

			if tt.expectError {
				assert.Error(t, err)
				assert.Len(t, sas.actives, 0)
			} else {
				assert.NoError(t, err)
				assert.Len(t, sas.actives, 1)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

func TestAnnouncementWriter_SendAnnouncement_WriteError(t *testing.T) {
	tests := map[string]struct {
		writeError   error
		expectAnnErr bool
	}{
		"stream error": {
			writeError: &StreamError{
				StreamID:  StreamID(123),
				ErrorCode: StreamErrorCode(42),
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
			mockStream := &MockQUICStream{}
			ctx := context.Background()

			mockStream.On("Context").Return(ctx)
			mockStream.On("Write", mock.Anything).Return(0, nil).Once()           // For init
			mockStream.On("Write", mock.Anything).Return(0, tt.writeError).Once() // For SendAnnouncement

			sas := newAnnouncementWriter(mockStream, "/test/")
			ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))

			// Initialize the AnnouncementWriter first
			err := sas.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = sas.SendAnnouncement(ann)

			assert.Error(t, err)

			if tt.expectAnnErr {
				var announceErr *AnnounceError
				assert.ErrorAs(t, err, &announceErr)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

func TestAnnouncementWriter_Close(t *testing.T) {
	t.Run("closes with no active announcements", func(t *testing.T) {
		mockStream := &MockQUICStream{}
		ctx := context.Background()

		mockStream.On("Context").Return(ctx)
		mockStream.On("Close").Return(nil)

		sas := newAnnouncementWriter(mockStream, "/test/")

		err := sas.Close()

		assert.NoError(t, err)
		assert.Nil(t, sas.actives)
		assert.NotNil(t, sas.initCh)

		mockStream.AssertExpectations(t)
	})

	t.Run("closes with active announcements", func(t *testing.T) {
		mockStream := &MockQUICStream{}
		ctx := context.Background()

		mockStream.On("Context").Return(ctx)
		mockStream.On("Write", mock.Anything).Return(0, nil) // For init and SendAnnouncement
		mockStream.On("Close").Return(nil)

		sas := newAnnouncementWriter(mockStream, "/test/")

		// Initialize the AnnouncementWriter first
		err := sas.init(map[*Announcement]struct{}{})
		require.NoError(t, err)

		// Add an active announcement
		ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/active"))
		err = sas.SendAnnouncement(ann)
		require.NoError(t, err)

		// Verify we have an active announcement
		assert.NotNil(t, sas.actives)
		assert.Len(t, sas.actives, 1)
		assert.True(t, ann.IsActive(), "announcement should be active initially")

		err = sas.Close()

		assert.NoError(t, err)
		assert.Nil(t, sas.actives)
		assert.True(t, ann.IsActive(), "announcement should remain active after Close (AnnouncementWriter doesn't end announcements)")

		mockStream.AssertExpectations(t)
	})

	t.Run("handles stream close error", func(t *testing.T) {
		mockStream := &MockQUICStream{}
		ctx := context.Background()

		expectedErr := fmt.Errorf("stream close error")
		mockStream.On("Context").Return(ctx)
		mockStream.On("Close").Return(expectedErr)

		sas := newAnnouncementWriter(mockStream, "/test/")

		err := sas.Close()

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, sas.actives)
		assert.NotNil(t, sas.initCh)

		mockStream.AssertExpectations(t)
	})
}

func TestAnnouncementWriter_CloseWithError(t *testing.T) {
	t.Run("closes with error and no active announcements", func(t *testing.T) {
		tests := map[string]struct {
			errorCode AnnounceErrorCode
		}{
			"internal error": {
				errorCode: InternalAnnounceErrorCode,
			},
			"duplicated announce error": {
				errorCode: DuplicatedAnnounceErrorCode,
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				mockStream := &MockQUICStream{}
				ctx := context.Background()

				mockStream.On("Context").Return(ctx)
				mockStream.On("CancelWrite", StreamErrorCode(tt.errorCode)).Return()
				mockStream.On("CancelRead", StreamErrorCode(tt.errorCode)).Return()

				sas := newAnnouncementWriter(mockStream, "/test/")

				err := sas.CloseWithError(tt.errorCode)

				assert.NoError(t, err)
				assert.Nil(t, sas.actives)
				assert.NotNil(t, sas.initCh)

				mockStream.AssertExpectations(t)
			})
		}
	})

	t.Run("closes with error and active announcements", func(t *testing.T) {
		mockStream := &MockQUICStream{}
		ctx := context.Background()

		mockStream.On("Context").Return(ctx)
		mockStream.On("Write", mock.Anything).Return(0, nil) // For init and SendAnnouncement
		mockStream.On("CancelWrite", StreamErrorCode(InternalAnnounceErrorCode)).Return()
		mockStream.On("CancelRead", StreamErrorCode(InternalAnnounceErrorCode)).Return()

		sas := newAnnouncementWriter(mockStream, "/test/")

		// Initialize the AnnouncementWriter first
		err := sas.init(map[*Announcement]struct{}{})
		require.NoError(t, err)

		// Add an active announcement
		ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/active"))
		err = sas.SendAnnouncement(ann)
		require.NoError(t, err)

		// Verify we have an active announcement
		assert.NotNil(t, sas.actives)
		assert.Len(t, sas.actives, 1)
		assert.True(t, ann.IsActive(), "announcement should be active initially")

		err = sas.CloseWithError(InternalAnnounceErrorCode)

		assert.NoError(t, err)
		assert.Nil(t, sas.actives)

		mockStream.AssertExpectations(t)
	})
}

func TestAnnouncementWriter_SendAnnouncement_MultipleAnnouncements(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil).Times(3) // One for init, two for SendAnnouncement

	sas := newAnnouncementWriter(mockStream, "/test/")

	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := sas.SendAnnouncement(ann1)
	err2 := sas.SendAnnouncement(ann2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, sas.actives, 2)
	assert.Contains(t, sas.actives, "stream1")
	assert.Contains(t, sas.actives, "stream2")
	assert.Equal(t, ann1, sas.actives["stream1"].announcement)
	assert.Equal(t, ann2, sas.actives["stream2"].announcement)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_SendAnnouncement_ReplaceExisting(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1")) // Same suffix

	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := sas.SendAnnouncement(ann1)
	assert.NoError(t, err1)

	// Send second announcement with same path - should replace the first
	err2 := sas.SendAnnouncement(ann2)
	assert.NoError(t, err2)

	// Should have only one active announcement (the newer one)
	assert.Len(t, sas.actives, 1)
	assert.Contains(t, sas.actives, "stream1")
	assert.Equal(t, ann2, sas.actives["stream1"].announcement)

	// First announcement should remain active (AnnouncementWriter doesn't end announcements)
	assert.True(t, ann1.IsActive())
	assert.True(t, ann2.IsActive())

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_SendAnnouncement_SameInstance(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil).Times(2) // One for init, one for SendAnnouncement

	sas := newAnnouncementWriter(mockStream, "/test/")
	ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err1 := sas.SendAnnouncement(ann)
	err2 := sas.SendAnnouncement(ann)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, sas.actives, 1)
	assert.True(t, ann.IsActive())

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_AnnouncementEnd_BackgroundProcessing(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil).Times(3) // init, ACTIVE, and ENDED messages

	sas := newAnnouncementWriter(mockStream, "/test/")
	ann, end := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	err = sas.SendAnnouncement(ann)
	assert.NoError(t, err)
	assert.Len(t, sas.actives, 1)

	end()

	// Wait for background goroutine to process and remove the active announcement
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n == 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n != 0 {
			t.Fatalf("timeout waiting for %d active announcements", 0)
		}
	}

	assert.Len(t, sas.actives, 0)

	mockStream.AssertExpectations(t)
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
			mockStream := &MockQUICStream{}
			ctx := context.Background()

			mockStream.On("Context").Return(ctx)
			if !tt.expectError {
				mockStream.On("Write", mock.Anything).Return(0, nil).Times(2) // One for init, one for SendAnnouncement
			} else {
				mockStream.On("Write", mock.Anything).Return(0, nil).Times(1) // Only for init
			}

			sas := newAnnouncementWriter(mockStream, tt.prefix)
			ann, _ := NewAnnouncement(ctx, BroadcastPath(tt.broadcastPath))

			// Initialize the AnnouncementWriter first
			err := sas.init(map[*Announcement]struct{}{})
			require.NoError(t, err)

			err = sas.SendAnnouncement(ann)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockStream.AssertExpectations(t)
		})
	}
}

func TestAnnouncementWriter_Performance_LargeNumberOfAnnouncements(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil) // Will handle init + multiple announcements

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	const numAnnouncements = 100 // Reduced for test efficiency

	start := time.Now()
	for i := range numAnnouncements {
		ann, _ := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/test/stream%d", i)))
		err := sas.SendAnnouncement(ann)
		assert.NoError(t, err)
	}
	duration := time.Since(start)

	t.Logf("Time to send %d announcements: %v", numAnnouncements, duration)
	assert.Len(t, sas.actives, numAnnouncements)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_CleanupResourceLeaks(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Create and end many announcements to test cleanup
	for i := range 10 {
		ann, end := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/test/stream%d", i)))
		err := sas.SendAnnouncement(ann)
		assert.NoError(t, err)
		end()
	}

	// Wait for cleanup to finish
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n == 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n != 0 {
			t.Fatalf("timeout waiting for %d active announcements", 0)
		}
	}
	assert.Len(t, sas.actives, 0)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_PartialCleanup(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Create multiple announcements
	ann1, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, end2 := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))
	ann3, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream3"))

	assert.NoError(t, sas.SendAnnouncement(ann1))
	assert.NoError(t, sas.SendAnnouncement(ann2))
	assert.NoError(t, sas.SendAnnouncement(ann3))

	// End only some announcements
	end2()

	// Wait for background processing to handle ended announcements
	// Inline: wait until sas.actives no longer contains "stream2"
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			_, ok := sas.actives["stream2"]
			sas.mu.RUnlock()
			if !ok {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		if _, ok := sas.actives["stream2"]; ok {
			sas.mu.RUnlock()
			t.Fatalf("timeout waiting for active announcements to not contain %s", "stream2")
		}
		sas.mu.RUnlock()
	}

	// Wait for background processing to reach exactly 2 active announcements
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n == 2 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n != 2 {
			t.Fatalf("timeout waiting for %d active announcements", 2)
		}
	}
	assert.Contains(t, sas.actives, "stream1")
	assert.NotContains(t, sas.actives, "stream2")
	assert.Contains(t, sas.actives, "stream3")

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_ConcurrentAccess(t *testing.T) {
	// NOTE: This test may occasionally cause deadlocks in the current implementation
	// when multiple goroutines compete for the same suffix, as the implementation
	// can deadlock between mutex acquisition and OnEnd callback processing.
	// The implementation should be fixed to avoid holding the mutex while calling End().

	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Test concurrent access to DIFFERENT suffixes to avoid deadlock
	done := make(chan bool, 2)
	errors := make(chan error, 2)

	go func() {
		defer func() { done <- true }()
		for i := range 5 {
			ann, _ := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/test/stream_a_%d", i)))
			if err := sas.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer func() { done <- true }()
		for i := range 5 {
			ann, _ := NewAnnouncement(ctx, BroadcastPath(fmt.Sprintf("/test/stream_b_%d", i)))
			if err := sas.SendAnnouncement(ann); err != nil {
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

	assert.Len(t, sas.actives, 10) // Should have 10 different streams

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_ConcurrentAccess_SameSuffix_DeadlockRisk(t *testing.T) {
	// This test verifies that concurrent access to the same suffix doesn't cause deadlock
	// after the implementation was fixed to use goroutines in OnEnd callbacks.

	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
	require.NoError(t, err)

	// Test concurrent access to the SAME suffix - this should no longer cause deadlock
	done := make(chan bool, 2)
	errors := make(chan error, 2)

	go func() {
		defer func() { done <- true }()
		for range 10 {
			ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
			if err := sas.SendAnnouncement(ann); err != nil {
				errors <- err
				return
			}
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer func() { done <- true }()
		for range 10 {
			ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
			if err := sas.SendAnnouncement(ann); err != nil {
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
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n == 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n != 1 {
			t.Fatalf("timeout waiting for %d active announcements", 1)
		}
	}

	close(errors)
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	assert.Len(t, sas.actives, 1)
	assert.Contains(t, sas.actives, "stream1")

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_MultipleClose(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Close").Return(nil).Times(2) // Allow multiple close calls

	sas := newAnnouncementWriter(mockStream, "/test/")

	err1 := sas.Close()
	assert.NoError(t, err1)
	assert.Nil(t, sas.actives)

	err2 := sas.Close()
	assert.NoError(t, err2)

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_Context(t *testing.T) {
	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)

	sas := newAnnouncementWriter(mockStream, "/test/")

	assert.NotNil(t, sas.Context())
	assert.NoError(t, sas.Context().Err())

	mockStream.AssertExpectations(t)
}

func TestAnnouncementWriter_StressTest_HeavyConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	mockStream := &MockQUICStream{}
	ctx := context.Background()

	mockStream.On("Context").Return(ctx)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	sas := newAnnouncementWriter(mockStream, "/test/")

	// Initialize the AnnouncementWriter first
	err := sas.init(map[*Announcement]struct{}{})
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

				ann, end := NewAnnouncement(ctx, BroadcastPath(suffixPath))
				if err := sas.SendAnnouncement(ann); err != nil {
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
	// Inline: wait until sas has at least 1 active announcement
	{
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			sas.mu.RLock()
			n := 0
			if sas.actives != nil {
				n = len(sas.actives)
			}
			sas.mu.RUnlock()
			if n >= 1 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		sas.mu.RLock()
		n := 0
		if sas.actives != nil {
			n = len(sas.actives)
		}
		sas.mu.RUnlock()
		if n < 1 {
			t.Fatalf("timeout waiting for at least %d active announcements", 1)
		}
	}

	close(errors)
	for err := range errors {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify that we still have some active announcements
	assert.True(t, len(sas.actives) > 0, "Should have some active announcements remaining")

	mockStream.AssertExpectations(t)
}
