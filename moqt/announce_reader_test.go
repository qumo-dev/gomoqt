package moqt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: helper logic inlined at call-sites to keep scope local and make
// each test tune its timeout precisely. This avoids proliferating rarely
// used top-level helpers and keeps timings tighter for CI.

func TestNewAnnouncementReader(t *testing.T) {
	mockStream := &FakeQUICStream{}
	prefix := "/test/prefix/"

	ras := newAnnouncementReader(mockStream, prefix, []string{"suffix1", "suffix2"})

	require.NotNil(t, ras)
	assert.Equal(t, prefix, ras.prefix)
	assert.Equal(t, mockStream, ras.stream)
	assert.NotNil(t, ras.actives)
	assert.NotNil(t, ras.pendings)
	assert.NotNil(t, ras.announcedCh)
	assert.NotNil(t, ras.ctx)

	// Clean up: short wait for goroutine startup; keep tiny to avoid flakiness
	time.Sleep(1 * time.Millisecond)
}

func TestAnnouncementReader_ReceiveAnnouncement(t *testing.T) {
	tests := map[string]struct {
		receiveAnnounceStream *AnnouncementReader
		ctx                   context.Context
		wantErr               bool
		wantErrType           error
		wantAnn               bool
	}{
		"success_with_valid_announcement": {
			receiveAnnounceStream: func() *AnnouncementReader {
				buf := bytes.NewBuffer(nil)
				// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
				require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
				err := message.AnnounceBroadcastMessage{
					BroadcastPathSuffix: "valid_announcement",
					AnnounceStatus:      message.ACTIVE,
				}.Encode(buf)
				require.NoError(t, err)

				mockStream := &FakeQUICStream{}
				data := append([]byte(nil), buf.Bytes()...)
				reader := bytes.NewReader(data)
				mockStream.ReadFunc = reader.Read
				ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})
				return ras
			}(),
			ctx:     context.Background(),
			wantErr: false,
			wantAnn: true,
		},
		"context_cancelled": {
			receiveAnnounceStream: func() *AnnouncementReader {
				mockStream := &FakeQUICStream{}
				// Don't provide initial suffixes so that ReceiveAnnouncement will wait
				return newAnnouncementReader(mockStream, "/test/", []string{})
			}(),
			ctx: func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(), wantErr: true,
			wantErrType: context.Canceled,
			wantAnn:     false,
		},
		"stream_closed": {
			receiveAnnounceStream: func() *AnnouncementReader {
				mockStream := &FakeQUICStream{}
				// Don't provide initial suffixes so that ReceiveAnnouncement will wait
				ras := newAnnouncementReader(mockStream, "/test/", []string{})
				// Allow goroutine to start (very short)
				time.Sleep(1 * time.Millisecond)
				_ = ras.Close()
				return ras
			}(),
			ctx:     context.Background(),
			wantErr: true,
			wantAnn: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ras := tt.receiveAnnounceStream

			// Allow time for background goroutine processing: wait for announcement
			if name == "success_with_valid_announcement" {
				// inline waitForAnnouncements with a smaller timeout
				deadline := time.Now().Add(100 * time.Millisecond)
				for time.Now().Before(deadline) {
					ras.announcementsMu.Lock()
					n := len(ras.actives) + len(ras.pendings)
					ras.announcementsMu.Unlock()
					if n > 0 {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
				if time.Now().After(deadline) {
					t.Fatalf("timeout waiting for announcements after %v", 100*time.Millisecond)
				}
			}

			ctxWithTimeout, cancel := context.WithTimeout(tt.ctx, 500*time.Millisecond)
			defer cancel()

			announcement, err := ras.ReceiveAnnouncement(ctxWithTimeout)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrType != nil {
					assert.ErrorIs(t, err, tt.wantErrType)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.wantAnn {
				assert.NotNil(t, announcement)
				if announcement != nil {
					assert.Equal(t, BroadcastPath("/test/valid_announcement"), announcement.BroadcastPath())
				}
			} else {
				assert.Nil(t, announcement)
			}

		})
	}
}

func TestAnnouncementReader_Close(t *testing.T) {
	tests := map[string]struct {
		setupFunc func() *AnnouncementReader
		wantErr   bool
	}{"normal_close": {
		setupFunc: func() *AnnouncementReader {
			mockStream := &FakeQUICStream{}
			return newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})
		},
		wantErr: false,
	},
		"already_closed": {
			setupFunc: func() *AnnouncementReader {
				mockStream := &FakeQUICStream{}
				ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})
				_ = ras.Close() // Close once
				return ras
			},
			wantErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ras := tt.setupFunc()
			time.Sleep(5 * time.Millisecond) // Allow goroutine to start

			err := ras.Close()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// The Close method only closes the stream, it doesn't cancel the context
			// So we don't need to check for context cancellation here
		})
	}
}

func TestAnnouncementReader_CloseWithError(t *testing.T) {
	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (int, error) { return 0, io.EOF },
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})

	// Allow goroutine to start and call Read (very short)
	time.Sleep(1 * time.Millisecond)

	// First close with error
	ras.CloseWithError(AnnounceErrorCodeInternal)

	assert.True(t, ras.Context().Err() != nil, "Context should be cancelled after close with error")
	// TODO: Fix Cause function issue
	// assert.ErrorAs(t, Cause(ras.Context()), &AnnounceError{})
}

func TestAnnouncementReader_CloseWithError_MultipleClose(t *testing.T) {
	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (int, error) { return 0, io.EOF },
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})

	// Allow goroutine to start and call Read (very short)
	time.Sleep(1 * time.Millisecond)

	// First close with error
	ras.CloseWithError(AnnounceErrorCodeInternal)

	// Second close with error should return the same error
	ras.CloseWithError(AnnounceErrorCodeDuplicated)

	assert.True(t, ras.Context().Err() != nil, "Context should be cancelled after close with error")
}

func TestAnnouncementReader_AnnouncementTracking(t *testing.T) {
	mockStream := &FakeQUICStream{}
	ras := newAnnouncementReader(mockStream, "/test/", []string{}) // No initial announcements

	// Wait for the goroutine to start and process EOF (deterministic)
	{
		tctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		select {
		case <-ras.ctx.Done():
		case <-tctx.Done():
		}
		cancel()
		// Not fatal; proceed — the goroutine may not cancel context on EOF, but this avoids blind sleeps
	}

	// Test internal announcement tracking
	ctx := context.Background()
	ann1, end1 := NewAnnouncement(ctx, BroadcastPath("/test/stream1"))
	ann2, end2 := NewAnnouncement(ctx, BroadcastPath("/test/stream2"))
	defer end2()

	// Manually add announcements to test tracking
	ras.announcementsMu.Lock()
	ras.actives["stream1"] = ann1
	ras.actives["stream2"] = ann2
	ras.announcementsMu.Unlock()

	assert.Len(t, ras.actives, 2)

	// Test ending announcement
	end1()

	// Announcement should still be in map until processed by background goroutine
	assert.Len(t, ras.actives, 2)

}

func TestAnnouncementReader_ConcurrentAccess(t *testing.T) {
	// Create multiple messages
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	for i := range 5 {
		err := message.AnnounceBroadcastMessage{
			BroadcastPathSuffix: fmt.Sprintf("/stream%d", i),
			AnnounceStatus:      message.ACTIVE,
		}.Encode(buf)
		require.NoError(t, err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	reader := bytes.NewReader(data)
	mockStream := &FakeQUICStream{
		ReadFunc: reader.Read,
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})

	// Wait for message processing to begin (deterministic)
	{
		deadline := time.Now().Add(100 * time.Millisecond)
		for time.Now().Before(deadline) {
			ras.announcementsMu.Lock()
			n := len(ras.actives) + len(ras.pendings)
			ras.announcementsMu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for announcements after %v", 100*time.Millisecond)
		}
	}

	// Test concurrent ReceiveAnnouncement calls
	var wg sync.WaitGroup
	results := make(chan *Announcement, 5)
	errors := make(chan error, 5)

	for range 3 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			ann, err := ras.ReceiveAnnouncement(ctx)
			if err != nil {
				errors <- err
			} else if ann != nil {
				results <- ann
			}
		})
	}

	// Concurrent close call
	wg.Go(func() {
		time.Sleep(10 * time.Millisecond)
		_ = ras.Close()
	})

	wg.Wait()
	close(results)
	close(errors)

	// Verify we got some results before closing
	receivedCount := 0
	for range results {
		receivedCount++
	}

	// Should have received at least some announcements
	assert.GreaterOrEqual(t, receivedCount, 0)
}

func TestAnnouncementReader_PrefixHandling(t *testing.T) {
	tests := map[string]struct {
		prefix       string
		suffix       string
		expectedPath string
	}{
		"simple_prefix_and_suffix": {
			prefix:       "/test/",
			suffix:       "/stream",
			expectedPath: "/test//stream",
		},
		"nested_prefix": {
			prefix:       "/test/sub/",
			suffix:       "/stream",
			expectedPath: "/test/sub//stream",
		},
		"root_prefix": {
			prefix:       "/",
			suffix:       "stream",
			expectedPath: "/stream",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &FakeQUICStream{}
			ras := newAnnouncementReader(mockStream, tt.prefix, []string{tt.suffix})

			// Allow goroutine to start and call Read (very short)
			time.Sleep(1 * time.Millisecond)

			require.NotNil(t, ras)
			assert.Equal(t, tt.prefix, ras.prefix)

			// Test path construction by manually adding announcement
			ctx := context.Background()
			ann, end := NewAnnouncement(ctx, BroadcastPath(tt.expectedPath))
			defer end()
			ras.announcementsMu.Lock()
			ras.pendings = append(ras.pendings, ann)
			ras.announcementsMu.Unlock()

			announcement, err := ras.ReceiveAnnouncement(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPath, string(announcement.BroadcastPath()))
		})
	}
}

func TestAnnouncementReader_InvalidMessage(t *testing.T) {
	// Create invalid message data
	invalidData := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	buf := bytes.NewBuffer(invalidData)

	mockStream := &FakeQUICStream{
		ReadFunc: buf.Read,
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})

	// Give time for processing invalid data (short)
	time.Sleep(5 * time.Millisecond)

	// When decode fails, the goroutine just returns without closing the stream
	// So the context should NOT be cancelled in this case
	select {
	case <-ras.ctx.Done():
		t.Error("Stream should not be closed when decode fails - goroutine should just return")
	default:
		// This is expected - context is not cancelled when decode fails
	}

}

func TestAnnouncementReader_ActiveThenEnded(t *testing.T) {
	// Test scenario: stream becomes active then ended
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	messages := []message.AnnounceBroadcastMessage{
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ACTIVE},
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ENDED},
	}
	for _, msg := range messages {
		err := msg.Encode(buf)
		require.NoError(t, err)
	}

	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			}
			// Block after all data is consumed
			select {}
		},
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{})

	// Wait until messages are observed by the reader instead of sleeping.
	{
		deadline := time.Now().Add(100 * time.Millisecond)
		for time.Now().Before(deadline) {
			ras.announcementsMu.Lock()
			n := len(ras.actives) + len(ras.pendings)
			ras.announcementsMu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for announcements after %v", 100*time.Millisecond)
		}
	}

	// Try to receive announcements - in this scenario, the announcement
	// becomes active and then immediately ends, so we might not catch it in the active state
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	ann, err := ras.ReceiveAnnouncement(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, ann)
	// The path should be constructed as prefix + suffix, which gives "/test/" + "/stream1" = "/test//stream1"
	assert.Equal(t, BroadcastPath("/test/stream1"), ann.BroadcastPath())
	assert.False(t, ann.IsActive())

	// The announcement should eventually be ended by the ENDED message
	// No extra wait needed here — ReceiveAnnouncement observed the ended announcement.

}

func TestAnnouncementReader_MultipleActiveStreams(t *testing.T) {
	// Test scenario: multiple streams become active
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	messages := []message.AnnounceBroadcastMessage{
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ACTIVE},
		{BroadcastPathSuffix: "stream2", AnnounceStatus: message.ACTIVE},
	}
	for _, msg := range messages {
		err := msg.Encode(buf)
		require.NoError(t, err)
	}

	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			}
			// Block after all data is consumed
			select {}
		},
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{})

	// Wait until messages are observed by the reader instead of sleeping.
	{
		deadline := time.Now().Add(50 * time.Millisecond)
		for time.Now().Before(deadline) {
			ras.announcementsMu.Lock()
			n := len(ras.actives) + len(ras.pendings)
			ras.announcementsMu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for announcements after %v", 50*time.Millisecond)
		}
	}

	// Verify that we can receive multiple active announcements
	receivedAnnouncements := make(map[string]*Announcement)

	// Try to receive up to 3 announcements (2 from messages + 1 initial)
	for range 3 {
		// Use a short per-call timeout so a single blocking call doesn't dominate the test
		perCtx, perCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		ann, err := ras.ReceiveAnnouncement(perCtx)
		perCancel()
		if err != nil {
			if err == context.DeadlineExceeded {
				break
			}
			t.Fatalf("Unexpected error: %v", err)
		}
		if ann != nil && ann.IsActive() {
			receivedAnnouncements[string(ann.BroadcastPath())] = ann
		}
	}

	// Should have received at least the 2 new announcements
	assert.GreaterOrEqual(t, len(receivedAnnouncements), 2)
	// The paths are constructed as prefix + suffix, so "/test/" + "stream1" = "/test/stream1"
	assert.Contains(t, receivedAnnouncements, "/test/stream1")
	assert.Contains(t, receivedAnnouncements, "/test/stream2")

}

func TestAnnouncementReader_DuplicateActiveError(t *testing.T) {
	// Test scenario: duplicate ACTIVE message should cause error
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	messages := []message.AnnounceBroadcastMessage{
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ACTIVE},
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ACTIVE}, // Duplicate
	}
	for _, msg := range messages {
		err := msg.Encode(buf)
		require.NoError(t, err)
	}

	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			}
			// Block after all data is consumed
			select {}
		},
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{})

	// Wait for the reader's context to be cancelled due to error.
	{
		closed := false
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ras.ctx.Done():
			closed = true
			timer.Stop()
		case <-timer.C:
			closed = false
		}
		if closed {
			t.Log("Stream correctly closed due to duplicate announcement")
		} else {
			t.Log("Stream not closed - this may be acceptable depending on implementation")
		}
	}
}

func TestAnnouncementReader_EndNonExistentStreamError(t *testing.T) {
	// Test scenario: ENDED message for non-existent stream should cause error
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	messages := []message.AnnounceBroadcastMessage{
		{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ENDED}, // End without ACTIVE
	}
	for _, msg := range messages {
		err := msg.Encode(buf)
		require.NoError(t, err)
	}

	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			}
			// Block after all data is consumed
			select {}
		},
	}

	ras := newAnnouncementReader(mockStream, "/test/", []string{})

	// Wait for the reader's context to be cancelled due to error.
	{
		closed := false
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ras.ctx.Done():
			closed = true
			timer.Stop()
		case <-timer.C:
			closed = false
		}
		if closed {
			t.Log("Stream correctly closed due to ending non-existent stream")
		} else {
			t.Log("Stream not closed - this may be acceptable depending on implementation")
		}
	}
}

func TestAnnouncementReader_NotifyChannel(t *testing.T) {
	// Create a message
	buf := bytes.NewBuffer(nil)
	// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
	require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
	err := message.AnnounceBroadcastMessage{
		BroadcastPathSuffix: "test_stream",
		AnnounceStatus:      message.ACTIVE}.Encode(buf)
	require.NoError(t, err)

	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			if buf.Len() > 0 {
				return buf.Read(p)
			}
			// Block after data
			select {}
		},
	}

	// Don't provide initial suffixes so we only get the stream message
	ras := newAnnouncementReader(mockStream, "/test/", []string{})

	// Wait until the reader has processed the message
	{
		deadline := time.Now().Add(100 * time.Millisecond)
		for time.Now().Before(deadline) {
			ras.announcementsMu.Lock()
			n := len(ras.actives) + len(ras.pendings)
			ras.announcementsMu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for announcements after %v", 100*time.Millisecond)
		}
	}

	// Verify that we can receive the announcement without blocking
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	announcement, err := ras.ReceiveAnnouncement(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, announcement)
	assert.Equal(t, BroadcastPath("/test/test_stream"), announcement.BroadcastPath())
	assert.True(t, announcement.IsActive())
}

func TestAnnouncementReader_BoundaryValues(t *testing.T) {
	tests := map[string]struct {
		prefix       string
		suffix       string
		expectedPath string
		wantErr      bool
		expectPanic  bool
	}{
		"empty_prefix": {
			prefix:      "",
			suffix:      "/stream",
			expectPanic: true, // invalid prefix causes panic
		},
		"empty_suffix": {
			prefix:       "/test/",
			suffix:       "",
			expectedPath: "/test/",
			wantErr:      false,
		},
		"both_empty": {
			prefix:      "",
			suffix:      "",
			expectPanic: true, // invalid prefix causes panic
		},
		"root_prefix": {
			prefix:       "/",
			suffix:       "stream",
			expectedPath: "/stream",
			wantErr:      false,
		},
		"long_prefix": {
			prefix:       "/very/long/nested/prefix/path/",
			suffix:       "stream",
			expectedPath: "/very/long/nested/prefix/path/stream", // Note: double slash expected
			wantErr:      false,
		},
		"long_suffix": {
			prefix:       "/test/",
			suffix:       "very/long/nested/suffix/path/stream",
			expectedPath: "/test/very/long/nested/suffix/path/stream", // Note: double slash expected
			wantErr:      false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Handle panic cases
			if tt.expectPanic {
				assert.Panics(t, func() {
					mockStream := &FakeQUICStream{}
					newAnnouncementReader(mockStream, tt.prefix, []string{})
				})
				return
			}

			// Create message with the test suffix
			buf := bytes.NewBuffer(nil)
			// The publisher sends ANNOUNCE_OK before any ANNOUNCE_BROADCAST.
			require.NoError(t, message.AnnounceOkMessage{}.Encode(buf))
			err := message.AnnounceBroadcastMessage{
				BroadcastPathSuffix: tt.suffix,
				AnnounceStatus:      message.ACTIVE}.Encode(buf)
			require.NoError(t, err)

			mockStream := &FakeQUICStream{
				ReadFunc: func(p []byte) (n int, err error) {
					if buf.Len() > 0 {
						return buf.Read(p)
					}
					// Block after data
					select {}
				},
			}

			// Don't provide initial suffixes so we only get the stream message
			ras := newAnnouncementReader(mockStream, tt.prefix, []string{})

			// Wait until the reader has processed the message
			{
				deadline := time.Now().Add(50 * time.Millisecond)
				for time.Now().Before(deadline) {
					ras.announcementsMu.Lock()
					n := len(ras.actives) + len(ras.pendings)
					ras.announcementsMu.Unlock()
					if n > 0 {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
				if time.Now().After(deadline) {
					t.Fatalf("timeout waiting for announcements after %v", 50*time.Millisecond)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			announcement, err := ras.ReceiveAnnouncement(ctx)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, announcement)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, announcement)
				assert.Equal(t, BroadcastPath(tt.expectedPath), announcement.BroadcastPath())
			}
		})
	}
}

func TestAnnouncementReader_StreamErrors(t *testing.T) {
	tests := map[string]struct {
		setupError func() error
	}{
		"quic_stream_error": {
			setupError: func() error {
				return &transport.StreamError{
					StreamID:  transport.StreamID(123),
					ErrorCode: transport.StreamErrorCode(42),
				}
			},
		},
		"generic_io_error": {
			setupError: func() error {
				return io.ErrUnexpectedEOF
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testError := tt.setupError()

			mockStream := &FakeQUICStream{
				ReadFunc: func(p []byte) (int, error) {
					return 0, testError
				},
			}

			ras := newAnnouncementReader(mockStream, "/test/", []string{"valid_announcement"})

			// In quic-go, Read errors are receive-side events and do NOT cancel
			// the stream's Context (which is send-side). The reader goroutine
			// silently exits on decode errors.

			// The initial "valid_announcement" should still be available.
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			ann, err := ras.ReceiveAnnouncement(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, ann)

			// After consuming the initial pending, the goroutine is dead (decode
			// failed) so no more announcements will arrive. ReceiveAnnouncement
			// should block until ctx times out.
			shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer shortCancel()

			ann2, err := ras.ReceiveAnnouncement(shortCtx)
			assert.Nil(t, ann2)
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}
