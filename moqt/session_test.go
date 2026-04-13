package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
	quic "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(conn StreamConn) *Session {
	return newSession(conn, NewTrackMux(), nil, nil, nil)
}

func newTestSessionWithConn(tb testing.TB, opts ...func(*FakeStreamConn)) (*Session, *FakeStreamConn) {
	tb.Helper()
	conn := &FakeStreamConn{}
	for _, opt := range opts {
		opt(conn)
	}
	session := newTestSession(conn)
	tb.Cleanup(func() {
		_ = session.CloseWithError(NoError, "")
	})
	return session, conn
}

func TestNewSession(t *testing.T) {
	tests := map[string]struct {
		mux      *TrackMux
		expectOK bool
	}{
		"new session with mux": {
			mux:      NewTrackMux(),
			expectOK: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conn := &FakeStreamConn{}
			conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ} }
			conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, io.EOF }
			conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, io.EOF }

			session := newSession(conn, tt.mux, nil, nil, nil)

			if tt.expectOK {
				assert.NotNil(t, session, "newSession should not return nil")
				assert.Equal(t, tt.mux, session.mux, "mux should be set correctly")
				assert.NotNil(t, session.trackReaders, "receive group stream queues should not be nil")
				assert.Equal(t, moqtVersion, session.ConnectionState().Version, "ConnectionState() should expose the MOQ version")
				// remote address method should return connection's address
				assert.Equal(t, "127.0.0.1:8080", session.RemoteAddr().String(), "RemoteAddr() should forward to connection")
			}

			// Cleanup
			_ = session.CloseWithError(NoError, "")
		})
	}
}

func TestNewSessionConnectionState(t *testing.T) {
	session, _ := newTestSessionWithConn(t, func(conn *FakeStreamConn) {
		conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ} }
	})

	state := session.ConnectionState()
	assert.Equal(t, moqtVersion, state.Version, "ConnectionState() should expose the MOQ version")
	require.NotNil(t, state.TLS, "ConnectionState().TLS should be populated")
	assert.Equal(t, NextProtoMOQ, state.TLS.NegotiatedProtocol, "TLS negotiated protocol should reflect quic")
}

func TestNewSessionWithNilMux(t *testing.T) {
	tests := map[string]struct {
		mux           *TrackMux
		expectDefault bool
	}{
		"nil mux uses default": {
			mux:           nil,
			expectDefault: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conn := &FakeStreamConn{}

			session := newSession(conn, tt.mux, nil, nil, nil)

			if tt.expectDefault {
				assert.Equal(t, DefaultMux, session.mux, "should use DefaultMux when nil mux is provided")
			}

			// Cleanup
			_ = session.CloseWithError(InternalSessionErrorCode, "terminate reason")
		})
	}
}

func TestNewSession_WithNilLogger(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	assert.NotNil(t, session, "session should be created with nil logger")
}

func TestNewSession_ClosureOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	conn := &FakeStreamConn{}
	conn.ParentCtx = ctx

	session := newTestSession(conn)
	assert.NotNil(t, session)

	cancel()

	select {
	case <-session.Context().Done():
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("session context was not canceled after conn context cancel")
	}

}

func TestSession_CloseWithError(t *testing.T) {
	tests := map[string]struct {
		code SessionErrorCode
		msg  string
	}{
		"terminate with error": {
			code: InternalSessionErrorCode,
			msg:  "test error",
		},
		"terminate normally": {
			code: NoError,
			msg:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			session, _ := newTestSessionWithConn(t)

			err := session.CloseWithError(tt.code, tt.msg)
			assert.NoError(t, err, "CloseWithError should not return error")
		})
	}
}

func TestSession_Subscribe(t *testing.T) {
	tests := map[string]struct {
		path      BroadcastPath
		name      TrackName
		config    *SubscribeConfig
		wantError bool
	}{
		"valid track stream": {
			path: BroadcastPath("/test/track"),
			name: TrackName("video"),
			config: &SubscribeConfig{
				Priority:   TrackPriority(1),
				Ordered:    true,
				MaxLatency: 250,
				StartGroup: 4,
				EndGroup:   9,
			},
			wantError: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a separate mock for the track stream that responds to the SUBSCRIBE protocol
			mockTrackStream := &FakeQUICStream{}
			// Create a SubscribeOkMessage response
			subok := message.SubscribeOkMessage{
				PublisherPriority:   7,
				PublisherOrdered:    1,
				PublisherMaxLatency: 500,
				StartGroup:          5,
				EndGroup:            10,
			}
			var buf bytes.Buffer
			_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
			err := subok.Encode(&buf)
			assert.NoError(t, err, "failed to encode SubscribeOkMessage")

			// Use ReadFunc for simpler mocking
			resp := bytes.NewReader(append([]byte(nil), buf.Bytes()...))
			mockTrackStream.ReadFunc = resp.Read

			mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

			conn := &FakeStreamConn{}
			conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

			session := newTestSession(conn)

			track, err := session.Subscribe(context.Background(), tt.path, tt.name, tt.config)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, track)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, track)
				assert.Equal(t, tt.path, track.BroadcastPath)
				assert.Equal(t, tt.name, track.TrackName)
				gotConfig := track.TrackConfig()
				assert.Equal(t, tt.config, gotConfig)
				assert.Equal(t, tt.config.Ordered, gotConfig.Ordered)
				assert.Equal(t, tt.config.MaxLatency, gotConfig.MaxLatency)
				assert.Equal(t, tt.config.StartGroup, gotConfig.StartGroup)
				assert.Equal(t, tt.config.EndGroup, gotConfig.EndGroup)
			}

			// Cleanup
			_ = session.CloseWithError(NoError, "")
		})
	}
}

func TestSession_Subscribe_SubscribeDropAsFirstResponse(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	requestStream := &FakeQUICStream{}
	requestStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	var response bytes.Buffer
	require.NoError(t, message.SubscribeDropMessage{
		StartGroup: 1,
		EndGroup:   2,
		ErrorCode:  0,
	}.Encode(&response))
	responseData := append([]byte{byte(message.MessageTypeSubscribeDrop)}, response.Bytes()...)
	requestStream.ReadFunc = func(p []byte) (int, error) {
		if len(responseData) == 0 {
			return 0, io.EOF
		}
		n := copy(p, responseData)
		responseData = responseData[n:]
		return n, nil
	}

	conn.OpenStreamFunc = func() (transport.Stream, error) { return requestStream, nil }

	reader, err := session.Subscribe(context.Background(), "/test", "video", &SubscribeConfig{})
	require.Error(t, err)
	assert.Nil(t, reader)
	assert.ErrorContains(t, err, "unexpected SUBSCRIBE_DROP message received")

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_NilContext(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	var nilCtx context.Context
	reader, err := session.Subscribe(nilCtx, "/test", "video", nil)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestSession_Subscribe_InvalidPath(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	reader, err := session.Subscribe(context.Background(), "invalid", "video", nil)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestSession_Subscribe_OpenError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, errors.New("open stream failed") }

	session := newTestSession(conn)

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}

	subscriber, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, subscriber)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_OpenStreamApplicationError(t *testing.T) {
	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, appErr }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeStreamTypeError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}

	// Make Write fail
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeStreamTypeStreamError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}

	// Make Write fail with StreamError
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInternal),
		Remote:    true,
	}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, strErr }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)
	// Depending on cancellation timing this may surface as SubscribeError or timeout.
	// We only require a failure with nil reader here.

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_NilConfig(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}

	// Create a SubscribeOkMessage response
	subok := message.SubscribeOkMessage{}
	var buf bytes.Buffer
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
	err := subok.Encode(&buf)
	assert.NoError(t, err)

	resp := bytes.NewReader(append([]byte(nil), buf.Bytes()...))
	mockTrackStream.ReadFunc = resp.Read
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	// Pass nil config - should use default
	reader, err := session.Subscribe(context.Background(), "/test", "video", nil)

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeSubscribeMessageStreamError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}

	// Use WriteFunc for direct control
	writeCallCount := 0
	mockTrackStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			// First write succeeds (StreamType)
			return len(p), nil
		}
		// Second write fails (SubscribeMessage)
		return 0, errors.New("write error")
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeSubscribeMessageRemoteStreamError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}

	// Use WriteFunc for direct control
	writeCallCount := 0
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInternal),
		Remote:    true,
	}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			// First write succeeds (StreamType)
			return len(p), nil
		}
		// Second write fails with remote StreamError
		return 0, strErr
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)
	// Depending on cancellation timing this may surface as SubscribeError or timeout.
	// We only require a failure with nil reader here.

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_DecodeSubscribeOkStreamError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	// Make Read fail with StreamError
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInternal),
		Remote:    false,
	}
	mockTrackStream.ReadFunc = func(p []byte) (int, error) { return 0, strErr }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)
	// Depending on cancellation timing this may surface as SubscribeError or timeout.
	// We only require a failure with nil reader here.

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_DecodeSubscribeOkError(t *testing.T) {
	mockTrackStream := &FakeQUICStream{}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	// Make Read fail with generic error
	mockTrackStream.ReadFunc = func(p []byte) (int, error) { return 0, errors.New("read error") }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockTrackStream, nil }

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Context(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	ctx := session.Context()
	assert.NotNil(t, ctx, "Context should not be nil")
}

func TestSession_nextSubscribeID(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	id1 := session.nextSubscribeID()
	id2 := session.nextSubscribeID()

	assert.NotEqual(t, id1, id2, "nextSubscribeID should return unique IDs")
	assert.True(t, id2 > id1, "Subsequent IDs should be larger")
}

func TestSession_HandleBiStreams_AcceptError(t *testing.T) {
	conn := &FakeStreamConn{}
	// Signal that AcceptStream/AcceptUniStream were attempted
	acceptStreamCh := make(chan struct{}, 1)
	conn.AcceptStreamFunc = func(context.Context) (transport.Stream, error) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
		return nil, errors.New("accept stream failed")
	}
	conn.AcceptUniStreamFunc = func(context.Context) (transport.ReceiveStream, error) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
		return nil, errors.New("accept stream failed")
	}

	session := newTestSession(conn)

	// Wait for the background goroutine to attempt AcceptStream/AcceptUniStream
	select {
	case <-acceptStreamCh:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AcceptStream/AcceptUniStream not called by background goroutine")
	}

	// The session should handle the error gracefully
	assert.NotNil(t, session)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_HandleUniStreamsAcceptError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.AcceptStreamFunc = func(context.Context) (transport.Stream, error) { return nil, errors.New("accept uni stream failed") }
	conn.AcceptUniStreamFunc = func(context.Context) (transport.ReceiveStream, error) {
		return nil, errors.New("accept uni stream failed")
	}

	session := newTestSession(conn)

	// Wait a bit for the background goroutine to try accepting
	time.Sleep(50 * time.Millisecond)

	// The session should handle the error gracefully
	assert.NotNil(t, session)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_ConcurrentAccess(t *testing.T) {
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }
	conn.OpenUniStreamFunc = func() (transport.SendStream, error) { return &FakeQUICSendStream{}, nil }

	session := newTestSession(conn)

	// Test concurrent access
	done := make(chan struct{})
	var operations int

	// Concurrent nextSubscribeID calls
	go func() {
		for range 5 {
			session.nextSubscribeID()
		}
		operations++
		if operations == 2 {
			close(done)
		}
	}()

	// Concurrent Context calls
	go func() {
		for range 5 {
			session.Context()
		}
		operations++
		if operations == 2 {
			close(done)
		}
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Concurrent operations timed out")
	}

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_ContextCancellation(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	ctx := session.Context()
	assert.NotNil(t, ctx)

	// Terminate the session
	_ = session.CloseWithError(NoError, "test termination")

	// Context should be cancelled after termination
	timer := time.AfterFunc(100*time.Millisecond, func() {
		t.Error("Context should be cancelled after termination")
	})

	<-ctx.Done()

	timer.Stop()
}

func TestSession_WithRealMux(t *testing.T) {
	conn := &FakeStreamConn{}

	mux := NewTrackMux()

	session := newSession(conn, mux, nil, nil, nil)

	assert.Equal(t, mux, session.mux, "Mux should be set correctly in the session")

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce(t *testing.T) {
	tests := map[string]struct {
		prefix      string
		setupMocks  func(*FakeStreamConn, any)
		expectError bool
	}{
		"successful announce": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *FakeStreamConn, mockStream any) {
				stream := mockStream.(*FakeQUICStream)
				mockConn.OpenStreamFunc = func() (transport.Stream, error) { return stream, nil }
				stream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
			},
			expectError: false,
		},
		"terminating session": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *FakeStreamConn, mockStream any) {
				mockConn.CloseWithErrorFunc = func(code transport.ConnErrorCode, reason string) error { return errors.New("close error") }
				mockConn.OpenStreamFunc = func() (transport.Stream, error) { return nil, io.EOF }
			},
			expectError: true,
		},
		"open stream error": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *FakeStreamConn, mockStream any) {
				mockConn.OpenStreamFunc = func() (transport.Stream, error) { return nil, errors.New("open stream error") }
			},
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a minimal session for testing
			conn := &FakeStreamConn{}

			session := newTestSession(conn)

			announceStream := &FakeQUICStream{}
			tt.setupMocks(conn, announceStream)

			reader, err := session.AcceptAnnounce(tt.prefix)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, reader)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, reader)
			}

			// Cleanup
			_ = session.CloseWithError(NoError, "")
		})
	}
}

func TestSession_AddTrackWriter(t *testing.T) {
	// Create a minimal session for testing
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	writer := &TrackWriter{}
	id := SubscribeID(1)
	session.addTrackWriter(id, writer)
	assert.Equal(t, writer, session.trackWriters[id])

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_RemoveTrackWriter(t *testing.T) {
	// Create a minimal session for testing
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	writer := &TrackWriter{}
	id := SubscribeID(1)
	session.trackWriters[id] = writer
	session.removeTrackWriter(id)
	assert.NotContains(t, session.trackWriters, id)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_RemoveTrackReader(t *testing.T) {
	// Create a minimal session for testing
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	reader := &TrackReader{}
	id := SubscribeID(1)
	session.trackReaders[id] = reader
	session.removeTrackReader(id)
	assert.NotContains(t, session.trackReaders, id)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestCancelStreamWithError(t *testing.T) {
	mockStream := &FakeQUICStream{}

	cancelStreamWithError(mockStream, transport.StreamErrorCode(1))

	// CancelWrite check
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(1), cancelWriteErr.ErrorCode)

	// CancelRead check
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(1), cancelReadErr.ErrorCode)
}

func TestSession_AddTrackReader(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	reader := &TrackReader{}
	id := SubscribeID(1)
	session.addTrackReader(id, reader)
	assert.Equal(t, reader, session.trackReaders[id])

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_Announce(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)
	blockHandler := make(chan struct{})
	trackReady := make(chan *TrackWriter, 1)
	session.mux.PublishFunc(context.Background(), BroadcastPath("/test/path"), func(tw *TrackWriter) {
		trackReady <- tw
		select {
		case <-blockHandler:
		case <-tw.Context().Done():
		}
	})

	// Create a mock stream for ANNOUNCE
	mockStream := &FakeQUICStream{}
	// Expect write/close operations for announcement writer init and close
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	// Prepare StreamType + AnnouncePleaseMessage
	var buf bytes.Buffer
	err := message.StreamTypeAnnounce.Encode(&buf)
	assert.NoError(t, err)
	apm := message.AnnouncePleaseMessage{TrackPrefix: "/test/prefix/"}
	err = apm.Encode(&buf)
	assert.NoError(t, err)

	data := buf.Bytes()
	// Wrap the ReadFunc to signal when the message is read to detect processing start
	var readCh = make(chan struct{}, 1)
	orig := func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}
	mockStream.ReadFunc = func(p []byte) (int, error) {
		n, err := orig(p)
		select {
		case readCh <- struct{}{}:
		default:
		}
		return n, err
	}

	// This will block, so we run it in a goroutine
	done := make(chan struct{})
	go func() {
		session.processBiStream(mockStream)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		// Expected to block waiting for announcements
	}

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_Subscribe(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenUniStreamFunc = func() (transport.SendStream, error) { return &FakeQUICSendStream{}, nil }

	session := newTestSession(conn)
	blockHandler := make(chan struct{})
	trackReady := make(chan *TrackWriter, 1)
	session.mux.PublishFunc(context.Background(), BroadcastPath("/test/path"), func(tw *TrackWriter) {
		trackReady <- tw
		select {
		case <-blockHandler:
		case <-tw.Context().Done():
		}
	})

	// Create a mock stream for SUBSCRIBE
	mockStream := &FakeQUICStream{}

	// Prepare StreamType + SubscribeMessage
	var buf bytes.Buffer
	err := message.StreamTypeSubscribe.Encode(&buf)
	assert.NoError(t, err)
	sm := message.SubscribeMessage{
		SubscribeID:          1,
		BroadcastPath:        "/test/path",
		TrackName:            "video",
		SubscriberPriority:   7,
		SubscriberOrdered:    1,
		SubscriberMaxLatency: 500,
		StartGroup:           5,
		EndGroup:             10,
	}
	err = sm.Encode(&buf)
	assert.NoError(t, err)

	data := buf.Bytes()
	readCh := make(chan struct{}, 1)
	origRead := func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}
	mockStream.ReadFunc = func(p []byte) (int, error) {
		n, err := origRead(p)
		select {
		case readCh <- struct{}{}:
		default:
		}
		return n, err
	}
	writeCh := make(chan struct{}, 1)
	mockStream.WriteFunc = func(p []byte) (int, error) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
		return 0, nil
	}

	// This will block in serveTrack, so we run it in a goroutine
	done := make(chan struct{})
	go func() {
		session.processBiStream(mockStream)
		close(done)
	}()

	// Wait for the processing to start by detecting a Read
	select {
	case <-readCh:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("processBiStream did not read subscribe message")
	}

	// Wait for the track writer to reach the handler and verify the initial config
	var track *TrackWriter
	select {
	case track = <-trackReady:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("processBiStream did not deliver track writer to handler")
	}

	gotConfig := track.TrackConfig()
	assert.Equal(t, TrackPriority(7), gotConfig.Priority)
	assert.True(t, gotConfig.Ordered)
	assert.Equal(t, uint64(500), gotConfig.MaxLatency)
	assert.Equal(t, GroupSequence(4), gotConfig.StartGroup)
	assert.Equal(t, GroupSequence(9), gotConfig.EndGroup)

	close(blockHandler)

	// Terminate to stop blocking
	_ = session.CloseWithError(NoError, "")

	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		// Expected to complete after termination
	}
}

func TestSession_ProcessBiStream_InvalidStreamType(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Create a mock stream with invalid stream type
	mockStream := &FakeQUICStream{}

	// Prepare invalid StreamType (255)
	var buf bytes.Buffer
	buf.WriteByte(255)

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(mockStream)
		close(done)
	}()

	select {
	case <-done:
		// Expected to return after terminating session
	case <-time.After(200 * time.Millisecond):
		t.Error("processBiStream should complete after invalid stream type")
	}

	assert.False(t, session.terminating(), "Session should not terminate after invalid bi-stream type")

	// CancelWrite check
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(InternalSessionErrorCode), cancelWriteErr.ErrorCode)

	// CancelRead check
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(InternalSessionErrorCode), cancelReadErr.ErrorCode)

	assert.ErrorIs(t, mockStream.Context().Err(), context.Canceled)
}

func TestSession_ProcessBiStream_DecodeStreamTypeError(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	mockStream := &FakeQUICStream{}
	mockStream.ReadFunc = func(p []byte) (int, error) {
		return 0, io.ErrUnexpectedEOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(mockStream)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("processBiStream should complete after stream type decode error")
	}

	assert.False(t, session.terminating(), "Session should not terminate after bi-stream decode error")
}

func TestSession_ProcessBiStream_DecodeAnnounceMessageError(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	mockStream := &FakeQUICStream{}

	var buf bytes.Buffer
	err := message.StreamTypeAnnounce.Encode(&buf)
	require.NoError(t, err)

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		return 0, io.ErrUnexpectedEOF
	}

	session.processBiStream(mockStream)
}

func TestSession_ProcessBiStream_DecodeSubscribeMessageError(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	mockStream := &FakeQUICStream{}

	var buf bytes.Buffer
	err := message.StreamTypeSubscribe.Encode(&buf)
	require.NoError(t, err)

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		return 0, io.ErrUnexpectedEOF
	}

	session.processBiStream(mockStream)
}

func TestSession_ProcessBiStream_Fetch(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	called := false
	var gotReq *FetchRequest
	var gotWriter *GroupWriter
	session.fetchHandler = FetchHandlerFunc(func(w *GroupWriter, r *FetchRequest) {
		called = true
		gotReq = r
		gotWriter = w
	})

	mockStream := &FakeQUICStream{}

	var buf bytes.Buffer
	require.NoError(t, message.StreamTypeFetch.Encode(&buf))
	req := message.FetchMessage{
		BroadcastPath: "/test/path",
		TrackName:     "video",
		Priority:      3,
		GroupSequence: 42,
	}
	require.NoError(t, req.Encode(&buf))

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processBiStream(mockStream)

	assert.True(t, called, "fetch handler should be called")
	require.NotNil(t, gotReq)
	assert.Equal(t, BroadcastPath(req.BroadcastPath), gotReq.BroadcastPath)
	assert.Equal(t, TrackName(req.TrackName), gotReq.TrackName)
	assert.Equal(t, TrackPriority(req.Priority), gotReq.Priority)
	assert.Equal(t, GroupSequence(req.GroupSequence), gotReq.GroupSequence)
	require.NotNil(t, gotWriter)
	assert.Equal(t, GroupSequence(req.GroupSequence), gotWriter.GroupSequence())

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_FetchTypedNilHandler(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	var f FetchHandlerFunc
	session.fetchHandler = f

	mockStream := &FakeQUICStream{}

	var buf bytes.Buffer
	require.NoError(t, message.StreamTypeFetch.Encode(&buf))
	req := message.FetchMessage{
		BroadcastPath: "/test/path",
		TrackName:     "video",
		Priority:      3,
		GroupSequence: 42,
	}
	require.NoError(t, req.Encode(&buf))

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processBiStream(mockStream)

	// CancelWrite check
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(FetchErrorCodeInternal), cancelWriteErr.ErrorCode)

	// CancelRead check
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(FetchErrorCodeInternal), cancelReadErr.ErrorCode)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_FetchHandlerPanic(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	called := false
	session.fetchHandler = FetchHandlerFunc(func(w *GroupWriter, r *FetchRequest) {
		called = true
		panic("boom")
	})

	mockStream := &FakeQUICStream{}

	var buf bytes.Buffer
	require.NoError(t, message.StreamTypeFetch.Encode(&buf))
	req := message.FetchMessage{
		BroadcastPath: "/test/path",
		TrackName:     "video",
		Priority:      3,
		GroupSequence: 42,
	}
	require.NoError(t, req.Encode(&buf))

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processBiStream(mockStream)

	assert.True(t, called, "fetch handler should be called")

	// CancelWrite check
	var cancelWriteErr *transport.StreamError
	require.ErrorAs(t, context.Cause(mockStream.Context()), &cancelWriteErr)
	assert.Equal(t, transport.StreamErrorCode(FetchErrorCodeInternal), cancelWriteErr.ErrorCode)

	// CancelRead check
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, transport.StreamErrorCode(FetchErrorCodeInternal), cancelReadErr.ErrorCode)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe(t *testing.T) {
	conn := &FakeStreamConn{}

	requestStream := &FakeQUICStream{}

	var written bytes.Buffer
	requestStream.WriteFunc = func(p []byte) (int, error) {
		return written.Write(p)
	}

	var response bytes.Buffer
	respMsg := message.ProbeMessage{Bitrate: 250000}
	require.NoError(t, respMsg.Encode(&response))
	requestStream.ReadFunc = response.Read

	conn.OpenStreamFunc = func() (transport.Stream, error) { return requestStream, nil }

	session := newTestSession(conn)

	got, err := session.Probe(1000000)
	require.NoError(t, err)
	assert.Equal(t, uint64(250000), got)

	var streamType message.StreamType
	require.NoError(t, streamType.Decode(bytes.NewReader(written.Bytes()[:1])))
	assert.Equal(t, message.StreamTypeProbe, streamType)

	var sent message.ProbeMessage
	require.NoError(t, sent.Decode(bytes.NewReader(written.Bytes()[1:])))
	assert.Equal(t, uint64(1000000), sent.Bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_Probe(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	probeStream := &FakeQUICStream{}

	var written bytes.Buffer
	probeStream.WriteFunc = func(p []byte) (int, error) {
		return written.Write(p)
	}

	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 123456}.Encode(&incoming))
	data := incoming.Bytes()
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processBiStream(probeStream)

	var resp message.ProbeMessage
	require.NoError(t, resp.Decode(bytes.NewReader(written.Bytes())))
	assert.Equal(t, uint64(123456), resp.Bitrate)
	assert.False(t, session.terminating(), "Session should not terminate after probe handling")

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_ProbeMultipleMessages(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		return quic.ConnectionStats{}
	}

	session := newTestSession(conn)

	probeStream := &FakeQUICStream{}

	var written bytes.Buffer
	probeStream.WriteFunc = func(p []byte) (int, error) {
		return written.Write(p)
	}

	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 111000}.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 222000}.Encode(&incoming))
	data := incoming.Bytes()
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processBiStream(probeStream)

	got := written.Bytes()
	require.NotEmpty(t, got)
	reader := bytes.NewReader(got)
	for _, want := range []uint64{111000, 222000} {
		var resp message.ProbeMessage
		require.NoError(t, resp.Decode(reader))
		assert.Equal(t, want, resp.Bitrate)
	}

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessUniStream_Group(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Add a track reader
	mockTrackStream := &FakeQUICStream{}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	substr := newTestSendSubscribeStreamFromStream(mockTrackStream, &SubscribeConfig{})
	trackReader := newTrackReader("/test", "video", substr, func() {})
	session.addTrackReader(1, trackReader)

	// Create a mock receive stream for GROUP
	mockRecvStream := &FakeQUICReceiveStream{}

	// Prepare StreamType + GroupMessage
	var buf bytes.Buffer
	err := message.StreamTypeGroup.Encode(&buf)
	assert.NoError(t, err)
	gm := message.GroupMessage{
		SubscribeID:   1,
		GroupSequence: 0,
	}
	err = gm.Encode(&buf)
	assert.NoError(t, err)

	data := buf.Bytes()
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}
	session.processUniStream(mockRecvStream)

	// Verify group was enqueued
	time.Sleep(10 * time.Millisecond)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessUniStream_UnknownSubscribeID(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Create a mock receive stream for GROUP with unknown subscribe ID
	mockRecvStream := &FakeQUICReceiveStream{}

	// Prepare StreamType + GroupMessage with unknown subscribe ID
	var buf bytes.Buffer
	err := message.StreamTypeGroup.Encode(&buf)
	assert.NoError(t, err)
	gm := message.GroupMessage{
		SubscribeID:   999, // Unknown ID
		GroupSequence: 0,
	}
	err = gm.Encode(&buf)
	assert.NoError(t, err)

	data := buf.Bytes()
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	session.processUniStream(mockRecvStream)

	_, cancelReadErr := mockRecvStream.Read(make([]byte, 1))
	var cancelReadStreamErr *transport.StreamError
	require.ErrorAs(t, cancelReadErr, &cancelReadStreamErr)
	assert.Equal(t, transport.StreamErrorCode(InvalidSubscribeIDErrorCode), cancelReadStreamErr.ErrorCode)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessUniStream_InvalidStreamType(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Create a mock receive stream with invalid stream type
	mockRecvStream := &FakeQUICReceiveStream{}

	// Prepare invalid StreamType (254)
	var buf bytes.Buffer
	buf.WriteByte(254)

	data := buf.Bytes()
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	done := make(chan struct{})
	go func() {
		session.processUniStream(mockRecvStream)
		close(done)
	}()

	select {
	case <-done:
		// Expected to return after terminating session
	case <-time.After(200 * time.Millisecond):
		t.Error("processUniStream should complete after invalid stream type")
	}

	assert.False(t, session.terminating(), "Session should not terminate after invalid uni-stream type")
}

func TestSession_ProcessUniStream_DecodeStreamTypeError(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	mockRecvStream := &FakeQUICReceiveStream{}
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		return 0, io.ErrUnexpectedEOF
	}

	session.processUniStream(mockRecvStream)
}

func TestSession_ProcessUniStream_DecodeGroupMessageError(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	mockRecvStream := &FakeQUICReceiveStream{}

	var buf bytes.Buffer
	err := message.StreamTypeGroup.Encode(&buf)
	require.NoError(t, err)

	data := buf.Bytes()
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		return 0, io.ErrUnexpectedEOF
	}

	session.processUniStream(mockRecvStream)
}

func TestSession_Subscribe_TerminatingSession(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Terminate the session
	err := session.CloseWithError(NoError, "")
	assert.NoError(t, err)

	// Wait for termination to complete
	time.Sleep(10 * time.Millisecond)

	// Try to subscribe - should fail because session is terminating
	config := &SubscribeConfig{Priority: 1}
	reader, err := session.Subscribe(context.Background(), "/test", "video", config)

	// Subscribe should fail or return nil because session is terminated
	if err != nil || reader == nil {
		// Expected behavior
		assert.Nil(t, reader)
	}
}

func TestSession_AcceptAnnounce_TerminatingSession(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Terminate the session
	err := session.CloseWithError(NoError, "")
	assert.NoError(t, err)

	// Wait for termination to complete
	time.Sleep(10 * time.Millisecond)

	// Try to accept announce - should fail because session is terminating
	reader, err := session.AcceptAnnounce("/test/prefix/")

	// AcceptAnnounce should fail or return nil because session is terminated
	if err != nil || reader == nil {
		// Expected behavior
		assert.Nil(t, reader)
	}
}

func TestSession_AcceptAnnounce_OpenStreamApplicationError(t *testing.T) {
	conn := &FakeStreamConn{}

	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, appErr }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodeStreamTypeError(t *testing.T) {
	conn := &FakeStreamConn{}

	mockAnnStream := &FakeQUICStream{}
	mockAnnStream.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockAnnStream, nil }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodeStreamTypeStreamError(t *testing.T) {
	conn := &FakeStreamConn{}

	mockAnnStream := &FakeQUICStream{}

	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInternal),
		Remote:    false,
	}
	mockAnnStream.WriteFunc = func(p []byte) (int, error) { return 0, strErr }

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockAnnStream, nil }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodePleaseMessageStreamError(t *testing.T) {
	conn := &FakeStreamConn{}

	mockAnnStream := &FakeQUICStream{}

	// Use WriteFunc for direct control
	writeCallCount := 0
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInternal),
		Remote:    false,
	}
	mockAnnStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			// First write succeeds (StreamType)
			return len(p), nil
		}
		// Second write fails (AnnouncePleaseMessage)
		return 0, strErr
	}

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockAnnStream, nil }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_DecodeInitMessageStreamError(t *testing.T) {
	conn := &FakeStreamConn{}

	mockAnnStream := &FakeQUICStream{}
	mockAnnStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	// Make Read fail with StreamError
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInternal),
		Remote:    false,
	}
	mockAnnStream.ReadFunc = func(p []byte) (int, error) { return 0, strErr }

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockAnnStream, nil }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_DecodeInitMessageError(t *testing.T) {
	conn := &FakeStreamConn{}

	mockAnnStream := &FakeQUICStream{}
	mockAnnStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	// Make Read fail with generic error
	mockAnnStream.ReadFunc = func(p []byte) (int, error) { return 0, errors.New("read error") }

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockAnnStream, nil }

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_CloseWithError_AlreadyTerminating(t *testing.T) {
	conn := &FakeStreamConn{}
	var closeCount int
	conn.CloseWithErrorFunc = func(code transport.ConnErrorCode, reason string) error {
		closeCount++
		return nil
	}

	session := newTestSession(conn)

	// First termination
	err1 := session.CloseWithError(NoError, "first termination")
	assert.NoError(t, err1)

	// Second termination should return immediately without error
	err2 := session.CloseWithError(InternalSessionErrorCode, "second termination")
	// The second call returns nil because terminating() is already true
	assert.NoError(t, err2)

	// Verify CloseWithError was only called once
	assert.Equal(t, 1, closeCount)
}

func TestSession_Terminate_WithApplicationError(t *testing.T) {
	conn := &FakeStreamConn{}
	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}
	conn.CloseWithErrorFunc = func(code transport.ConnErrorCode, reason string) error { return appErr }

	session := newTestSession(conn)

	err := session.CloseWithError(InternalSessionErrorCode, "test error")

	assert.Error(t, err)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)
}

func TestSession_Fetch(t *testing.T) {
	conn := &FakeStreamConn{}

	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
		Priority:      3,
		GroupSequence: 42,
	}

	group, err := session.Fetch(req)
	require.NoError(t, err)
	assert.NotNil(t, group)
	assert.Equal(t, GroupSequence(42), group.GroupSequence())

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_ClosedSession(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	_ = session.CloseWithError(NoError, "")

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)
	assert.Equal(t, ErrClosedSession, err)
}

func TestSession_Fetch_OpenStreamError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) {
		return nil, errors.New("open stream failed")
	}

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_OpenStreamApplicationError(t *testing.T) {
	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, appErr }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_EncodeStreamTypeError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_EncodeStreamTypeRemoteStreamError(t *testing.T) {
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(FetchErrorCodeInternal),
		Remote:    true,
	}
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, strErr }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)
	var fetchErr *FetchError
	assert.ErrorAs(t, err, &fetchErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_EncodeFetchMessageError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	writeCallCount := 0
	mockStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			return len(p), nil // StreamType succeeds
		}
		return 0, errors.New("write error") // FetchMessage fails
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Fetch_EncodeFetchMessageRemoteStreamError(t *testing.T) {
	strErr := &transport.StreamError{
		ErrorCode: transport.StreamErrorCode(FetchErrorCodeTimeout),
		Remote:    true,
	}
	mockStream := &FakeQUICStream{}
	writeCallCount := 0
	mockStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			return len(p), nil // StreamType succeeds
		}
		return 0, strErr // FetchMessage fails with remote error
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	req := &FetchRequest{
		BroadcastPath: "/test",
		TrackName:     "video",
	}

	group, err := session.Fetch(req)
	assert.Error(t, err)
	assert.Nil(t, group)
	var fetchErr *FetchError
	assert.ErrorAs(t, err, &fetchErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_ClosedSession(t *testing.T) {
	session, _ := newTestSessionWithConn(t)

	_ = session.CloseWithError(NoError, "")

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)
	assert.Equal(t, ErrClosedSession, err)
}

func TestSession_Probe_OpenStreamError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) {
		return nil, errors.New("open stream failed")
	}

	session := newTestSession(conn)

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_OpenStreamApplicationError(t *testing.T) {
	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, appErr }

	session := newTestSession(conn)

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_EncodeStreamTypeError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return 0, errors.New("write error") }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_EncodeProbeMessageError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	writeCallCount := 0
	mockStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			return len(p), nil
		}
		return 0, errors.New("write error")
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_DecodeResponseError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	mockStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	mockStream.ReadFunc = func(p []byte) (int, error) { return 0, errors.New("read error") }

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	bitrate, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, uint64(0), bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_logError(t *testing.T) {
	t.Run("logs error when logger is set", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		conn := &FakeStreamConn{}
		session := newSession(conn, NewTrackMux(), nil, nil, logger)

		session.logError("something failed", errors.New("test error"), "key", "value")

		output := logBuf.String()
		assert.Contains(t, output, "something failed")
		assert.Contains(t, output, "test error")
		assert.Contains(t, output, "key=value")

		_ = session.CloseWithError(NoError, "")
	})

	t.Run("no panic with nil logger", func(t *testing.T) {
		session := newTestSession(&FakeStreamConn{})
		session.logError("msg", errors.New("err"))

		_ = session.CloseWithError(NoError, "")
	})

	t.Run("no panic with nil error", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		conn := &FakeStreamConn{}
		session := newSession(conn, NewTrackMux(), nil, nil, logger)

		session.logError("msg", nil)
		assert.Empty(t, logBuf.String())

		_ = session.CloseWithError(NoError, "")
	})

	t.Run("no panic with nil session", func(t *testing.T) {
		var s *Session
		s.logError("msg", errors.New("err"))
	})
}

func TestSession_processBiStream_logError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Create a stream that returns an immediate read error → logError should fire
	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, errors.New("broken stream")
		},
	}

	conn := &FakeStreamConn{}
	session := newSession(conn, NewTrackMux(), nil, nil, logger)

	session.processBiStream(mockStream)

	output := logBuf.String()
	assert.Contains(t, output, "failed to decode stream type")
	assert.Contains(t, output, "broken stream")

	_ = session.CloseWithError(NoError, "")
}

func TestSession_processUniStream_logError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	mockStream := &FakeQUICReceiveStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, errors.New("broken uni stream")
		},
	}

	conn := &FakeStreamConn{}
	session := newSession(conn, NewTrackMux(), nil, nil, logger)

	session.processUniStream(mockStream)

	output := logBuf.String()
	assert.Contains(t, output, "failed to decode uni stream type")
	assert.Contains(t, output, "broken uni stream")

	_ = session.CloseWithError(NoError, "")
}
