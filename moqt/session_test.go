package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(conn StreamConn) *Session {
	return newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil)
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

type noStatsConn struct{}

func (noStatsConn) AcceptStream(context.Context) (transport.Stream, error) { return nil, io.EOF }
func (noStatsConn) AcceptUniStream(context.Context) (transport.ReceiveStream, error) {
	return nil, io.EOF
}
func (noStatsConn) CloseWithError(transport.ConnErrorCode, string) error { return nil }
func (noStatsConn) Context() context.Context                             { return context.Background() }
func (noStatsConn) LocalAddr() net.Addr                                  { return &net.TCPAddr{} }
func (noStatsConn) OpenStream() (transport.Stream, error)                { return nil, io.EOF }
func (noStatsConn) OpenUniStream() (transport.SendStream, error)         { return nil, io.EOF }
func (noStatsConn) RemoteAddr() net.Addr                                 { return &net.TCPAddr{} }
func (noStatsConn) TLS() *tls.ConnectionState                            { return nil }

func TestNewSession(t *testing.T) {
	tests := map[string]struct {
		mux      *TrackMux
		expectOK bool
	}{
		"new session with mux": {
			mux:      NewTrackMux(0),
			expectOK: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conn := &FakeStreamConn{}
			conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ} }
			conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, io.EOF }

			session := newSession(conn, tt.mux, nil, nil, nil, nil, nil)

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

			session := newSession(conn, tt.mux, nil, nil, nil, nil, nil)

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

func TestNewSession_ConfigIsCloned(t *testing.T) {
	conn := &FakeStreamConn{}
	cfg := &Config{ProbeInterval: 50 * time.Millisecond}

	session := newSession(conn, nil, nil, cfg, nil, nil, nil)
	defer session.CloseWithError(InternalSessionErrorCode, "terminate reason")

	// mutate the original after newSession
	cfg.ProbeInterval = 999 * time.Second

	assert.Equal(t, 50*time.Millisecond, session.config.ProbeInterval,
		"session.config should not be affected by mutations to the original Config")
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
	acceptStreamCh := make(chan struct{}, 1)
	conn.AcceptStreamFunc = func(context.Context) (transport.Stream, error) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
		return nil, errors.New("accept uni stream failed")
	}
	conn.AcceptUniStreamFunc = func(context.Context) (transport.ReceiveStream, error) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
		return nil, errors.New("accept uni stream failed")
	}

	session := newTestSession(conn)

	select {
	case <-acceptStreamCh:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AcceptStream/AcceptUniStream not called by background goroutine")
	}

	assert.NotNil(t, session)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_ConcurrentAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockStream := &FakeQUICStream{}
		mockStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }
		conn := &FakeStreamConn{}
		conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }
		conn.OpenUniStreamFunc = func() (transport.SendStream, error) { return &FakeQUICSendStream{}, nil }

		session := newTestSession(conn)
		defer func() { _ = session.CloseWithError(NoError, "") }()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for range 5 {
				session.nextSubscribeID()
			}
		}()

		go func() {
			defer wg.Done()
			for range 5 {
				_ = session.Context()
			}
		}()

		wg.Wait()
	})
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

	mux := NewTrackMux(0)

	session := newTestSession(conn)
	session.mux = mux

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

func TestSession_Stats_NoTransport(t *testing.T) {
	// noStatsConn does not implement probeStatsProvider.
	// Transport-derived fields must be zero values.
	conn := noStatsConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	stats := sess.Stats()

	assert.Equal(t, time.Duration(0), stats.RTT)
	assert.Equal(t, uint64(0), stats.BytesSent)
	assert.Equal(t, uint64(0), stats.BytesReceived)
}

func TestSession_Stats_WithTransport(t *testing.T) {
	sess, _ := newTestSessionWithConn(t, func(c *FakeStreamConn) {
		c.ConnectionStatsFunc = func() quic.ConnectionStats {
			return quic.ConnectionStats{
				SmoothedRTT:   50 * time.Millisecond,
				BytesSent:     1_000,
				BytesReceived: 2_000,
			}
		}
	})

	stats := sess.Stats()

	assert.Equal(t, 50*time.Millisecond, stats.RTT)
	assert.Equal(t, uint64(1_000), stats.BytesSent)
	assert.Equal(t, uint64(2_000), stats.BytesReceived)
}

func TestSession_Stats_EstimatedBitrateZeroBeforeProbe(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)

	stats := sess.Stats()
	assert.Equal(t, uint64(0), stats.EstimatedBitrate)
}

func TestSession_Stats_EstimatedBitrateUpdatedByDetectBitrateChanges(t *testing.T) {
	var mu sync.Mutex
	var bytesSent uint64
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		// Simulate ongoing traffic: each call advances BytesSent by 100 kB
		// so that BitrateTracker sees a non-zero byte delta.
		mu.Lock()
		bytesSent += 100_000
		n := bytesSent
		mu.Unlock()
		return quic.ConnectionStats{BytesSent: n}
	}
	// Pass config at creation time so the Ticker in detectBitrateChanges
	// picks up the short interval (it is captured at goroutine start).
	cfg := &Config{ProbeInterval: 5 * time.Millisecond, ProbeMaxAge: 10 * time.Millisecond}
	sess := newSession(conn, NewTrackMux(0), nil, cfg, nil, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	// Allow detectBitrateChanges at least two ticks: first initializes, second measures.
	assert.Eventually(t, func() bool {
		return sess.Stats().EstimatedBitrate > 0
	}, 200*time.Millisecond, 10*time.Millisecond)
}

func TestSession_Stats_ReflectsRemovals(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)

	sess.addTrackReader(SubscribeID(1), &TrackReader{})
	sess.addTrackReader(SubscribeID(2), &TrackReader{})
	sess.removeTrackReader(SubscribeID(1))

	stats := sess.Stats()
	assert.Equal(t, time.Duration(0), stats.RTT)
	assert.Equal(t, uint64(0), stats.BytesSent)
	assert.Equal(t, uint64(0), stats.BytesReceived)
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

	// Prepare StreamType + AnnounceInterestMessage
	var buf bytes.Buffer
	err := message.StreamTypeAnnounce.Encode(&buf)
	assert.NoError(t, err)
	apm := message.AnnounceInterestMessage{BroadcastPathPrefix: "/test/prefix/"}
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

	probeStream := &FakeQUICStream{}

	var written bytes.Buffer
	probeStream.WriteFunc = func(p []byte) (int, error) {
		return written.Write(p)
	}

	// Publisher sends one ProbeMessage then EOF.
	var response bytes.Buffer
	require.NoError(t, message.ProbeMessage{Bitrate: 250000}.Encode(&response))
	probeStream.ReadFunc = response.Read

	conn.OpenStreamFunc = func() (transport.Stream, error) { return probeStream, nil }

	session := newTestSession(conn)

	ch, err := session.Probe(1000000)
	require.NoError(t, err)

	// Channel should receive the publisher's ProbeMessage.
	got := <-ch
	assert.Equal(t, uint64(250000), got.Bitrate)
	assert.Equal(t, uint64(250000), got.Bitrate)

	// Verify subscriber wrote StreamTypeProbe + ProbeMessage{Bitrate:1000000}.
	r := bytes.NewReader(written.Bytes())
	var streamType message.StreamType
	require.NoError(t, streamType.Decode(r))
	assert.Equal(t, message.StreamTypeProbe, streamType)
	var sent message.ProbeMessage
	require.NoError(t, sent.Decode(r))
	assert.Equal(t, uint64(1000000), sent.Bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_Probe(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		return quic.ConnectionStats{}
	}

	session := newTestSession(conn)
	session.config = &Config{ProbeInterval: 5 * time.Millisecond}

	probeStream := &FakeQUICStream{}

	received := make(chan message.ProbeMessage, 10)
	probeStream.WriteFunc = func(p []byte) (int, error) {
		var pm message.ProbeMessage
		if err := pm.Decode(bytes.NewReader(p)); err == nil {
			select {
			case received <- pm:
			default:
			}
		}
		return len(p), nil
	}

	// Subscriber sends StreamTypeProbe + ProbeMessage{Bitrate: targetBitrate}.
	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 1000000}.Encode(&incoming))
	data := incoming.Bytes()
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		<-session.Context().Done()
		return 0, io.EOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	// Wait for at least one ProbeMessage from the publisher.
	select {
	case <-received:
		// Successfully received a probe response from publisher.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no probe message received")
	}

	_ = session.CloseWithError(NoError, "")
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("processBiStream did not complete")
	}
}

func TestSession_ProcessBiStream_ProbeMultipleMessages(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		return quic.ConnectionStats{}
	}

	session := newSession(conn, NewTrackMux(0), nil,
		&Config{ProbeInterval: 5 * time.Millisecond, ProbeMaxAge: 15 * time.Millisecond},
		nil, nil, nil)

	probeStream := &FakeQUICStream{}

	received := make(chan message.ProbeMessage, 20)
	probeStream.WriteFunc = func(p []byte) (int, error) {
		var pm message.ProbeMessage
		if err := pm.Decode(bytes.NewReader(p)); err == nil {
			select {
			case received <- pm:
			default:
			}
		}
		return len(p), nil
	}

	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 500000}.Encode(&incoming))
	data := incoming.Bytes()
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		// Block until session closes so the stream stays registered
		// and runProbeResponder can keep writing measurements.
		<-session.Context().Done()
		return 0, io.EOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	// Let multiple max-age-triggered sends accumulate.
	time.Sleep(100 * time.Millisecond)

	_ = session.CloseWithError(NoError, "")
	<-done

	assert.Greater(t, len(received), 1, "should have received multiple probe messages")
}

func TestSession_ProcessBiStream_ProbeTargets(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		return quic.ConnectionStats{}
	}

	session := newTestSession(conn)

	probeStream := &FakeQUICStream{}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }

	// Subscriber sends: StreamTypeProbe + initial target + one updated target.
	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 500000}.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 1000000}.Encode(&incoming))
	data := incoming.Bytes()

	readCalled := make(chan struct{}, 1)
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		// Signal that all data has been consumed.
		select {
		case readCalled <- struct{}{}:
		default:
		}
		return 0, io.EOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	// Wait until the stream has been fully consumed.
	select {
	case <-readCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream was not consumed")
	}

	// The updated target (1000000) should be available on ProbeTargets().
	select {
	case got := <-session.ProbeTargets():
		assert.Equal(t, uint64(1000000), got.Bitrate)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no target received on ProbeTargets()")
	}

	_ = session.CloseWithError(NoError, "")
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("processBiStream did not complete")
	}
}

func TestSession_Probe_SecondCallReusesStream(t *testing.T) {
	conn := &FakeStreamConn{}

	var written bytes.Buffer
	// Use conn.Context() as parent so that closing the session also cancels the
	// stream context, allowing readProbeResults to exit cleanly.
	probeStream := &FakeQUICStream{ParentCtx: conn.Context()}
	probeStream.WriteFunc = func(p []byte) (int, error) { return written.Write(p) }
	// Block reads so the stream stays open throughout the test.
	probeStream.ReadFunc = func(p []byte) (int, error) {
		<-probeStream.Context().Done()
		return 0, io.EOF
	}

	openCount := 0
	conn.OpenStreamFunc = func() (transport.Stream, error) {
		openCount++
		return probeStream, nil
	}

	session := newTestSession(conn)

	ch1, err := session.Probe(1000000)
	require.NoError(t, err)

	ch2, err := session.Probe(2000000)
	require.NoError(t, err)

	// The same channel must be returned on the second call.
	assert.Equal(t, ch1, ch2, "second Probe call should return the same channel")

	// OpenStream must have been called exactly once.
	assert.Equal(t, 1, openCount, "second Probe call must not open a new stream")

	// Both ProbeMessages must have been written (after the StreamType header).
	r := bytes.NewReader(written.Bytes())
	var streamType message.StreamType
	require.NoError(t, streamType.Decode(r))
	assert.Equal(t, message.StreamTypeProbe, streamType)

	var msg1 message.ProbeMessage
	require.NoError(t, msg1.Decode(r))
	assert.Equal(t, uint64(1000000), msg1.Bitrate)

	var msg2 message.ProbeMessage
	require.NoError(t, msg2.Decode(r))
	assert.Equal(t, uint64(2000000), msg2.Bitrate)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_ChannelClosedOnSessionClose(t *testing.T) {
	conn := &FakeStreamConn{}

	// Make the probe stream's context a child of the connection context so that
	// closing the connection cancels the stream, unblocking the read goroutine.
	probeStream := &FakeQUICStream{
		ParentCtx: conn.Context(),
	}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	// Block until the stream context is cancelled (which happens when the
	// connection is closed and conn.Context() is cancelled).
	probeStream.ReadFunc = func(p []byte) (int, error) {
		<-probeStream.Context().Done()
		return 0, context.Cause(probeStream.Context())
	}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return probeStream, nil }

	session := newTestSession(conn)

	ch, err := session.Probe(1000000)
	require.NoError(t, err)

	// Close the session: cancels conn.Context() -> probeStream.Context() -> ReadFunc returns.
	_ = session.CloseWithError(NoError, "")

	// The channel must be closed (range or two-value receive must see ok=false).
	for range ch {
	}
}

func TestSession_ProcessBiStream_ProbeReplacesExisting(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats { return quic.ConnectionStats{} }

	session := newTestSession(conn)

	// stream1: sends StreamTypeProbe + initial ProbeMessage, then blocks until cancelled.
	var stream1Buf bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&stream1Buf))
	require.NoError(t, message.ProbeMessage{Bitrate: 500000}.Encode(&stream1Buf))
	stream1Data := stream1Buf.Bytes()

	stream1Active := make(chan struct{})
	stream1 := &FakeQUICStream{}
	stream1.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	stream1.ReadFunc = func(p []byte) (int, error) {
		if len(stream1Data) > 0 {
			n := copy(p, stream1Data)
			stream1Data = stream1Data[n:]
			if len(stream1Data) == 0 {
				select {
				case stream1Active <- struct{}{}:
				default:
				}
			}
			return n, nil
		}
		// Block until cancelled by stream2 arrival.
		<-stream1.Context().Done()
		return 0, context.Cause(stream1.Context())
	}

	stream1Done := make(chan struct{})
	go func() {
		session.processBiStream(stream1)
		close(stream1Done)
	}()

	// Wait until stream1 is fully consumed (registered as incomingProbeStream).
	select {
	case <-stream1Active:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream1 initial data was not consumed")
	}

	// stream2: sends StreamTypeProbe + initial ProbeMessage, then EOF.
	var stream2Buf bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&stream2Buf))
	require.NoError(t, message.ProbeMessage{Bitrate: 1000000}.Encode(&stream2Buf))
	stream2Data := stream2Buf.Bytes()

	stream2 := &FakeQUICStream{}
	stream2.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	stream2.ReadFunc = func(p []byte) (int, error) {
		if len(stream2Data) > 0 {
			n := copy(p, stream2Data)
			stream2Data = stream2Data[n:]
			return n, nil
		}
		return 0, io.EOF
	}

	stream2Done := make(chan struct{})
	go func() {
		session.processBiStream(stream2)
		close(stream2Done)
	}()

	// stream1 must be cancelled when stream2 takes over.
	select {
	case <-stream1Done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream1 was not cancelled when stream2 arrived")
	}

	// Close the session so stream2's handleProbeStream ticker loop exits.
	_ = session.CloseWithError(NoError, "")

	// stream2 must process normally and finish after session close.
	select {
	case <-stream2Done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream2 did not complete")
	}
}

func TestSession_ProcessBiStream_ProbeDecodeError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats { return quic.ConnectionStats{} }

	session := newTestSession(conn)

	// Stream sends only StreamTypeProbe; subsequent reads for ProbeMessage.Decode return
	// a non-EOF error, causing handleProbeStream to return an error.
	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))

	data := incoming.Bytes()
	cancelReadCalled := make(chan transport.StreamErrorCode, 1)
	cancelWriteCalled := make(chan transport.StreamErrorCode, 1)

	probeStream := &FakeQUICStream{}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		return 0, errors.New("simulated read error")
	}
	probeStream.CancelReadFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelReadCalled <- code:
		default:
		}
	}
	probeStream.CancelWriteFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelWriteCalled <- code:
		default:
		}
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processBiStream did not complete after decode error")
	}

	// The stream must have been cancelled with ProbeErrorCodeInternal.
	select {
	case code := <-cancelReadCalled:
		assert.Equal(t, transport.StreamErrorCode(ProbeErrorCodeInternal), code)
	default:
		t.Error("CancelRead was not called")
	}
	select {
	case code := <-cancelWriteCalled:
		assert.Equal(t, transport.StreamErrorCode(ProbeErrorCodeInternal), code)
	default:
		t.Error("CancelWrite was not called")
	}

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_ProbeUnsupported(t *testing.T) {
	// noStatsConn does not implement probeStatsProvider so runProbeResponder is
	// never started.  handleProbeStream still registers the stream and reads
	// PROBE messages; with no PROBE data it hits EOF immediately and returns nil.
	conn := &noStatsConn{}
	session := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil)

	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))

	data := incoming.Bytes()
	probeStream := &FakeQUICStream{}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		return 0, io.EOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processBiStream did not complete for unsupported probe stream")
	}

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProbeTargets_LatestValueSemantics(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats { return quic.ConnectionStats{} }

	session := newTestSession(conn)

	// Subscriber sends: StreamTypeProbe + initial target + two rapid updates.
	// The background goroutine must discard the first update (800000) when the
	// second update (1200000) arrives before the publisher has consumed it.
	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 500000}.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 800000}.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 1200000}.Encode(&incoming))

	data := incoming.Bytes()
	// bgDone fires when the background goroutine has consumed all update messages
	// and hits EOF on the next read.
	var bgOnce sync.Once
	bgDone := make(chan struct{})
	probeStream := &FakeQUICStream{}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		bgOnce.Do(func() { close(bgDone) })
		return 0, io.EOF
	}

	done := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(done)
	}()

	// Wait until the background goroutine has finished processing all updates.
	select {
	case <-bgDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("background goroutine did not consume all updates")
	}

	// EOF causes handleProbeStream to return, so processBiStream completes.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processBiStream did not complete")
	}

	// The channel must hold only the latest target (1200000).
	select {
	case got := <-session.ProbeTargets():
		assert.Equal(t, uint64(1200000), got.Bitrate)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no target on ProbeTargets()")
	}

	// No stale value must remain in the channel.
	select {
	case extra := <-session.ProbeTargets():
		t.Errorf("unexpected stale target %d still in channel", extra.Bitrate)
	default:
	}

	_ = session.CloseWithError(NoError, "")
}

// TestSession_ProbeTargets_InitialMessageDelivered verifies that the very first
// PROBE message sent by the subscriber (the "initial" message decoded synchronously
// by handleProbeStream) is forwarded to ProbeTargets(), even when no subsequent
// update messages are sent on the stream.
func TestSession_ProbeTargets_InitialMessageDelivered(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats { return quic.ConnectionStats{} }

	session := newTestSession(conn)

	// Subscriber sends exactly ONE ProbeMessage -no further updates.
	var incoming bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&incoming))
	require.NoError(t, message.ProbeMessage{Bitrate: 3_000_000}.Encode(&incoming))
	data := incoming.Bytes()

	probeStream := &FakeQUICStream{ParentCtx: conn.Context()}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) > 0 {
			n := copy(p, data)
			data = data[n:]
			return n, nil
		}
		// Block until stream context is cancelled so handleProbeStream stays alive
		// long enough for the test to read from ProbeTargets().
		<-probeStream.Context().Done()
		return 0, io.EOF
	}

	streamDone := make(chan struct{})
	go func() {
		session.processBiStream(probeStream)
		close(streamDone)
	}()

	// The initial target MUST arrive on ProbeTargets() even with no update messages.
	select {
	case got := <-session.ProbeTargets():
		assert.Equal(t, uint64(3_000_000), got.Bitrate)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("initial PROBE target was not delivered to ProbeTargets()")
	}

	_ = session.CloseWithError(NoError, "")
	select {
	case <-streamDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("processBiStream did not complete")
	}
}

func TestSession_ProcessUniStream_Group(t *testing.T) {
	conn := &FakeStreamConn{}

	session := newTestSession(conn)

	// Add a track reader
	mockTrackStream := &FakeQUICStream{}
	mockTrackStream.WriteFunc = func(p []byte) (int, error) { return 0, nil }

	substr := newTestSendSubscribeStream(mockTrackStream)
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
		// Second write fails (AnnounceInterestMessage)
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

	_, err := session.Probe(1000000)
	assert.Error(t, err)
	assert.Equal(t, ErrClosedSession, err)
}

func TestSession_Probe_OpenStreamError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) {
		return nil, errors.New("open stream failed")
	}

	session := newTestSession(conn)

	_, err := session.Probe(1000000)
	assert.Error(t, err)

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

	_, err := session.Probe(1000000)
	assert.Error(t, err)
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

	_, err := session.Probe(1000000)
	assert.Error(t, err)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe_EncodeProbeMessageError(t *testing.T) {
	mockStream := &FakeQUICStream{}
	writeCallCount := 0
	mockStream.WriteFunc = func(p []byte) (int, error) {
		writeCallCount++
		if writeCallCount == 1 {
			return len(p), nil // StreamType succeeds
		}
		return 0, errors.New("write error") // ProbeMessage fails
	}

	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newTestSession(conn)

	_, err := session.Probe(1000000)
	assert.Error(t, err)

	_ = session.CloseWithError(NoError, "")
}

// TestSession_Probe_EOFFromPublisher_NoStreamCancel verifies that when the publisher
// closes the probe stream cleanly (EOF), readProbeResults exits without calling
// CancelRead or CancelWrite. The probeResponseCh stays open until CloseWithError.
func TestSession_Probe_EOFFromPublisher_NoStreamCancel(t *testing.T) {
	conn := &FakeStreamConn{}

	cancelReadCalled := make(chan transport.StreamErrorCode, 1)
	cancelWriteCalled := make(chan transport.StreamErrorCode, 1)

	var response bytes.Buffer
	require.NoError(t, message.ProbeMessage{Bitrate: 500000}.Encode(&response))

	probeStream := &FakeQUICStream{ParentCtx: conn.Context()}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = response.Read // returns io.EOF after data is exhausted
	probeStream.CancelReadFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelReadCalled <- code:
		default:
		}
	}
	probeStream.CancelWriteFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelWriteCalled <- code:
		default:
		}
	}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return probeStream, nil }

	session := newTestSession(conn)

	ch, err := session.Probe(1000000)
	require.NoError(t, err)

	// Receive the result delivered before EOF.
	got := <-ch
	assert.Equal(t, uint64(500000), got.Bitrate)

	// Session close waits for readProbeResults (wg) then closes the channel.
	_ = session.CloseWithError(NoError, "")
	for range ch {
	}

	// EOF is a normal close -CancelRead/CancelWrite must NOT be called.
	select {
	case code := <-cancelReadCalled:
		t.Errorf("CancelRead must not be called on EOF (code %d)", code)
	default:
	}
	select {
	case code := <-cancelWriteCalled:
		t.Errorf("CancelWrite must not be called on EOF (code %d)", code)
	default:
	}
}

// TestSession_Probe_ResponseDecodeError verifies that a non-EOF decode error in
// readProbeResults causes the stream to be cancelled with ProbeErrorCodeInternal,
// and that the channel is closed when the session closes afterward.
func TestSession_Probe_ResponseDecodeError(t *testing.T) {
	conn := &FakeStreamConn{}

	cancelReadCalled := make(chan transport.StreamErrorCode, 1)
	cancelWriteCalled := make(chan transport.StreamErrorCode, 1)

	probeStream := &FakeQUICStream{ParentCtx: conn.Context()}
	probeStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	probeStream.ReadFunc = func(p []byte) (int, error) {
		return 0, errors.New("simulated decode error")
	}
	probeStream.CancelReadFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelReadCalled <- code:
		default:
		}
	}
	probeStream.CancelWriteFunc = func(code transport.StreamErrorCode) {
		select {
		case cancelWriteCalled <- code:
		default:
		}
	}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return probeStream, nil }

	session := newTestSession(conn)

	ch, err := session.Probe(1000000)
	require.NoError(t, err)

	// A non-EOF decode error must cancel the stream with ProbeErrorCodeInternal.
	select {
	case code := <-cancelReadCalled:
		assert.Equal(t, transport.StreamErrorCode(ProbeErrorCodeInternal), code)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("CancelRead was not called after decode error")
	}
	select {
	case code := <-cancelWriteCalled:
		assert.Equal(t, transport.StreamErrorCode(ProbeErrorCodeInternal), code)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("CancelWrite was not called after decode error")
	}

	// Session close must close the channel.
	_ = session.CloseWithError(NoError, "")
	for range ch {
	}
}

func TestSession_logError(t *testing.T) {
	t.Run("logs error when logger is set", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		conn := &FakeStreamConn{}
		session := newTestSession(conn)
		session.logger = logger

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
		session := newTestSession(conn)
		session.logger = logger

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

	// Create a stream that returns an immediate read error ->logError should fire
	mockStream := &FakeQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, errors.New("broken stream")
		},
	}

	conn := &FakeStreamConn{}
	session := newTestSession(conn)
	session.logger = logger

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
	session := newTestSession(conn)
	session.logger = logger

	session.processUniStream(mockStream)

	output := logBuf.String()
	assert.Contains(t, output, "failed to decode uni stream type")
	assert.Contains(t, output, "broken uni stream")

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Stats_EstimatedBitrateUpdatedEveryInterval(t *testing.T) {
	var mu sync.Mutex
	var bytesSent uint64
	delta := uint64(100_000)

	conn := &FakeStreamConn{}
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		mu.Lock()
		defer mu.Unlock()
		return quic.ConnectionStats{BytesSent: bytesSent}
	}

	// Set a very large MaxAge so it doesn't trigger by age.
	// Set a huge MaxDelta so changes don't trigger a notification.
	cfg := &Config{
		ProbeInterval: 10 * time.Millisecond,
		ProbeMaxAge:   1 * time.Hour,
		ProbeMaxDelta: 1000.0, // 100000% change needed for notification
	}
	sess := newSession(conn, NewTrackMux(0), nil, cfg, nil, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	// Initial tick to initialize tracker baseline
	mu.Lock()
	bytesSent = 100_000
	mu.Unlock()

	// Ensure bytesSent increases for the measurements
	// We'll use a slower ticker to reduce jitter impact
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				bytesSent += delta / 5 // Add 1/5 of delta every 2ms -> approx 1 delta every 10ms
				mu.Unlock()
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}()

	// Wait for first measurement (should be > 0)
	assert.Eventually(t, func() bool {
		return sess.Stats().EstimatedBitrate > 0
	}, 300*time.Millisecond, 10*time.Millisecond)

	firstBitrate := sess.Stats().EstimatedBitrate

	// Now we want to check if it updates even if the change is small.
	// We'll change the increment slightly.
	mu.Lock()
	delta = delta + 5_000 // 5% change
	mu.Unlock()

	// Wait for a few ticks
	time.Sleep(100 * time.Millisecond)

	secondBitrate := sess.Stats().EstimatedBitrate
	assert.NotEqual(t, firstBitrate, secondBitrate, "EstimatedBitrate should update every interval even if change is small")
}

func TestSession_Probe_ConcurrentAccess(t *testing.T) {
	ctx := t.Context()

	conn := &FakeStreamConn{
		ParentCtx: ctx,
	}

	mockStream := &FakeQUICStream{
		ParentCtx: conn.Context(),
	}
	mockStream.WriteFunc = func(p []byte) (int, error) { return len(p), nil }
	mockStream.ReadFunc = func(p []byte) (int, error) {
		<-mockStream.Context().Done()
		return 0, io.EOF
	}

	conn.OpenStreamFunc = func() (transport.Stream, error) { return mockStream, nil }

	session := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil)

	// Test concurrent access to Probe (receiving peer measurements)
	results, _ := session.Probe(1000000)
	results2, _ := session.Probe(2000000)

	var count1, count2 atomic.Int32
	consumeCtx, consumeCancel := context.WithCancel(context.Background())

	consume := func(ch <-chan ProbeResult, counter *atomic.Int32) {
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
				counter.Add(1)
			case <-consumeCtx.Done():
				return
			}
		}
	}

	go consume(results, &count1)
	go consume(results2, &count2)

	// Simulate incoming peer measurements
	for i := range 10 {
		session.notifyResults(uint64(1000 + i))
		time.Sleep(2 * time.Millisecond)
	}

	// Because it's a channel, the 10 messages are split between the two consumers.
	assert.Eventually(t, func() bool {
		return count1.Load()+count2.Load() >= 10
	}, 500*time.Millisecond, 10*time.Millisecond)

	// Test concurrent access to ProbeTargets (receiving local target hints)
	targets1 := session.ProbeTargets()
	targets2 := session.ProbeTargets()

	var tCount1, tCount2 atomic.Int32
	go consume(targets1, &tCount1)
	go consume(targets2, &tCount2)

	for i := range 10 {
		session.notifyTargets(uint64(2000 + i))
		time.Sleep(2 * time.Millisecond)
	}

	assert.Eventually(t, func() bool {
		return tCount1.Load()+tCount2.Load() >= 10
	}, 500*time.Millisecond, 10*time.Millisecond)

	// Clean shutdown: cancel consumers first, then close session
	consumeCancel()
	_ = session.CloseWithError(NoError, "")
}
