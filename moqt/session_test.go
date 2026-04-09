package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	quic "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testSubscribeRequest(tb testing.TB, config *SubscribeConfig) *SubscribeRequest {
	tb.Helper()
	req, err := NewSubscribeRequest("/test", "video", config)
	require.NoError(tb, err)
	return req
}

func newTestSession(conn StreamConn) *Session {
	return newSession(conn, NewTrackMux(), nil, nil)
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
			// Create a proper MockQUICStream
			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF) // Create a proper mock connection
			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			// Provide a default OpenStream behavior for tests that don't explicitly set it
			conn.On("OpenStream").Return(nil, io.EOF).Maybe()
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)    // For handleBiStreams goroutine
			conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // For handleUniStreams goroutine
			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
			// Provide a default OpenStream behavior for tests that don't explicitly set it
			conn.On("OpenStream").Return(nil, io.EOF).Maybe()

			session := newSession(conn, tt.mux, nil, nil)

			if tt.expectOK {
				assert.NotNil(t, session, "newSession should not return nil")
				assert.Equal(t, tt.mux, session.mux, "mux should be set correctly")
				assert.NotNil(t, session.trackReaders, "receive group stream queues should not be nil")
				// remote address method should return connection's address
				assert.Equal(t, "127.0.0.1:8080", session.RemoteAddr().String(), "RemoteAddr() should forward to connection")
			}

			// Cleanup
			_ = session.CloseWithError(NoError, "")
		})
	}
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
			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)
			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)    // For handleBiStreams goroutine
			conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // For handleUniStreams goroutine
			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			session := newSession(conn, tt.mux, nil, nil)

			if tt.expectDefault {
				assert.Equal(t, DefaultMux, session.mux, "should use DefaultMux when nil mux is provided")
			}

			// Cleanup
			_ = session.CloseWithError(InternalSessionErrorCode, "terminate reason")
		})
	}
}

func TestNewSession_WithNilLogger(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	assert.NotNil(t, session, "session should be created with nil logger")

	_ = session.CloseWithError(NoError, "")
}

func TestNewSession_ClosureOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(ctx)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}).Maybe()

	session := newTestSession(conn)
	assert.NotNil(t, session)

	cancel()

	select {
	case <-session.Context().Done():
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatal("session context was not canceled after conn context cancel")
	}

	conn.AssertExpectations(t)
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
			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)
			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)    // For handleBiStreams goroutine
			conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // For handleUniStreams goroutine
			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			session := newTestSession(conn)

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
			mockStream := &MockQUICStream{}
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)
			writeCh := make(chan struct{}, 1)
			mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(mock.Arguments) {
				select {
				case writeCh <- struct{}{}:
				default:
				}
			})
			mockStream.On("Close").Return(nil)
			mockStream.On("Context").Return(context.Background()) // Create a separate mock for the track stream that responds to the SUBSCRIBE protocol
			mockTrackStream := &MockQUICStream{}
			mockTrackStream.On("StreamID").Return(StreamID(2))
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

			mockTrackStream.On("Read", mock.Anything)
			mockTrackStream.On("Write", mock.Anything).Return(0, nil)
			mockTrackStream.On("Close").Return(nil)
			mockTrackStream.On("Context").Return(context.Background())

			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)    // For handleBiStreams goroutine
			conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // For handleUniStreams goroutine
			conn.On("OpenStream").Return(mockTrackStream, nil)
			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			session := newTestSession(conn)

			req, err := NewSubscribeRequest(tt.path, tt.name, tt.config)
			require.NoError(t, err)
			track, err := session.Subscribe(context.Background(), req)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, track)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, track)
				require.NotNil(t, track.Request)
				assert.Equal(t, tt.path, track.Request.BroadcastPath)
				assert.Equal(t, tt.name, track.Request.TrackName)
				assert.Equal(t, tt.config, track.Request.Config)
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

func TestSession_Subscribe_NilRequest(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	reader, err := session.Subscribe(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestSession_Subscribe_NilContext(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	reader, err := session.Subscribe(nil, testSubscribeRequest(t, nil))
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestSession_Subscribe_InvalidPath(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	req := &SubscribeRequest{BroadcastPath: "invalid", TrackName: "video"}
	reader, err := session.Subscribe(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestSession_Subscribe_OpenError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(nil, errors.New("open stream failed"))
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{
		Priority: TrackPriority(1),
	}

	subscriber, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, subscriber)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_OpenStreamApplicationError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	appErr := &ApplicationError{
		ErrorCode:    ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(nil, appErr)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeStreamTypeError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("CancelRead", mock.Anything).Return()
	mockTrackStream.On("CancelWrite", mock.Anything).Return()

	// Make Write fail
	mockTrackStream.On("Write", mock.Anything).Return(0, errors.New("write error"))

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeStreamTypeStreamError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("CancelRead", mock.Anything).Return()

	// Make Write fail with StreamError
	strErr := &StreamError{
		ErrorCode: StreamErrorCode(SubscribeErrorCodeInternal),
		Remote:    true,
	}
	mockTrackStream.On("Write", mock.Anything).Return(0, strErr)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)
	var subErr *SubscribeError
	assert.ErrorAs(t, err, &subErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_NilConfig(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	writeCh := make(chan struct{}, 1)
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())

	// Create a SubscribeOkMessage response
	subok := message.SubscribeOkMessage{}
	var buf bytes.Buffer
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
	err := subok.Encode(&buf)
	assert.NoError(t, err)

	resp := bytes.NewReader(append([]byte(nil), buf.Bytes()...))
	mockTrackStream.ReadFunc = resp.Read
	mockTrackStream.On("Read", mock.Anything)
	mockTrackStream.On("Write", mock.Anything).Return(0, nil)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	// Pass nil config - should use default
	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, nil))

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeSubscribeMessageStreamError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("CancelRead", mock.Anything).Return()
	mockTrackStream.On("CancelWrite", mock.Anything).Return()

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

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_EncodeSubscribeMessageRemoteStreamError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("CancelRead", mock.Anything).Return()

	// Use WriteFunc for direct control
	writeCallCount := 0
	strErr := &StreamError{
		ErrorCode: StreamErrorCode(SubscribeErrorCodeInternal),
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

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)
	var subErr *SubscribeError
	assert.ErrorAs(t, err, &subErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_DecodeSubscribeOkStreamError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("Write", mock.Anything).Return(0, nil)
	mockTrackStream.On("CancelWrite", mock.Anything).Return()

	// Make Read fail with StreamError
	strErr := &StreamError{
		ErrorCode: StreamErrorCode(SubscribeErrorCodeInternal),
		Remote:    false,
	}
	mockTrackStream.On("Read", mock.Anything).Return(0, strErr)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)
	var subErr *SubscribeError
	assert.ErrorAs(t, err, &subErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Subscribe_DecodeSubscribeOkError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("StreamID").Return(StreamID(2))
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("Write", mock.Anything).Return(0, nil)
	mockTrackStream.On("CancelWrite", mock.Anything).Return()
	// Do not expect CancelRead to be called for generic read errors.
	mockTrackStream.On("CancelRead", mock.Anything).Return().Maybe()

	// Make Read fail with generic error
	mockTrackStream.On("Read", mock.Anything).Return(0, errors.New("read error"))

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockTrackStream, nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	config := &SubscribeConfig{Priority: 1}

	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_Context(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	ctx := session.Context()
	assert.NotNil(t, ctx, "Context should not be nil")

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_nextSubscribeID(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	id1 := session.nextSubscribeID()
	id2 := session.nextSubscribeID()

	assert.NotEqual(t, id1, id2, "nextSubscribeID should return unique IDs")
	assert.True(t, id2 > id1, "Subsequent IDs should be larger")

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_HandleBiStreams_AcceptError(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	// Signal that AcceptStream/AcceptUniStream were attempted
	acceptStreamCh := make(chan struct{}, 1)
	conn.On("AcceptStream", mock.Anything).Return(nil, errors.New("accept stream failed")).Run(func(mock.Arguments) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
	})
	conn.On("AcceptUniStream", mock.Anything).Return(nil, errors.New("accept stream failed")).Run(func(mock.Arguments) {
		select {
		case acceptStreamCh <- struct{}{}:
		default:
		}
	})
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

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
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, errors.New("accept uni stream failed"))
	conn.On("AcceptUniStream", mock.Anything).Return(nil, errors.New("accept uni stream failed"))
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

	// Wait a bit for the background goroutine to try accepting
	time.Sleep(50 * time.Millisecond)

	// The session should handle the error gracefully
	assert.NotNil(t, session)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_ConcurrentAccess(t *testing.T) {
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Write", mock.Anything).Return(0, nil)
	mockStream.On("Close").Return(nil)
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	conn.On("OpenStream").Return(mockStream, nil)
	conn.On("OpenUniStream").Return(&MockQUICSendStream{})
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

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
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	// Create a cancellable context for the connection
	connCtx, connCancel := context.WithCancel(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(connCtx)
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		// Cancel the connection context when CloseWithError is called
		connCancel()
	}).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	session := newTestSession(conn)

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
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Context").Return(context.Background())

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	mux := NewTrackMux()

	session := newSession(conn, mux, nil, nil)

	assert.Equal(t, mux, session.mux, "Mux should be set correctly in the session")

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_GoAway(t *testing.T) {
	// Create a minimal session for testing
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("Write", mock.Anything).Return(0, nil)

	session := newTestSession(conn)

	// Test goAway - now it calls updateSession
	err := session.goAway("test-uri")
	assert.NoError(t, err)

	// Cleanup
	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce(t *testing.T) {
	tests := map[string]struct {
		prefix      string
		setupMocks  func(*MockStreamConn, any)
		expectError bool
	}{
		"successful announce": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *MockStreamConn, mockStream any) {
				mockConn.On("OpenStream").Return(mockStream, nil).Once()
				stream := mockStream.(*MockQUICStream)
				stream.On("Context").Return(context.Background()).Once()
				stream.On("StreamID").Return(StreamID(1)).Once()
				// Mock writes for StreamType and AnnouncePlease
				stream.On("Write", mock.AnythingOfType("[]uint8")).Return(0, nil).Times(2)
				stream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
			},
			expectError: false,
		},
		"terminating session": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *MockStreamConn, mockStream any) {
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(errors.New("close error")).Once()
				// Provide optional OpenStream behavior to avoid unexpected calls during termination
				mockConn.On("OpenStream").Return(nil, io.EOF).Maybe()
			},
			expectError: true,
		},
		"open stream error": {
			prefix: "/test/prefix/",
			setupMocks: func(mockConn *MockStreamConn, mockStream any) {
				mockConn.On("OpenStream").Return(nil, errors.New("open stream error")).Once()
			},
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a minimal session for testing
			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			if name != "terminating session" {
				conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			}

			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
			conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
			conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)

			session := newTestSession(conn)

			announceStream := &MockQUICStream{}
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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

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
	mockStream := &MockQUICStream{}
	mockStream.On("CancelRead", StreamErrorCode(1)).Return()
	mockStream.On("CancelWrite", StreamErrorCode(1)).Return()

	cancelStreamWithError(mockStream, StreamErrorCode(1))

	mockStream.AssertExpectations(t)
}

func TestSession_AddTrackReader(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader := &TrackReader{}
	id := SubscribeID(1)
	session.addTrackReader(id, reader)
	assert.Equal(t, reader, session.trackReaders[id])

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessBiStream_Announce(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Create a mock stream for ANNOUNCE
	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(1))
	mockStream.On("Context").Return(context.Background())
	// Expect write/close operations for announcement writer init and close
	mockStream.On("Write", mock.AnythingOfType("[]uint8")).Return(0, nil).Maybe()
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelRead", mock.Anything).Return(nil).Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return(nil).Maybe()

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("OpenUniStream").Return(&MockQUICSendStream{}, nil).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Create a mock stream for SUBSCRIBE
	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(2))
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	mockStream.On("CancelRead", mock.Anything).Return().Maybe()

	// Prepare StreamType + SubscribeMessage
	var buf bytes.Buffer
	err := message.StreamTypeSubscribe.Encode(&buf)
	assert.NoError(t, err)
	sm := message.SubscribeMessage{
		SubscribeID:   1,
		BroadcastPath: "/test/path",
		TrackName:     "video",
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
	mockStream.On("Write", mock.Anything).Return(0, nil).Run(func(mock.Arguments) {
		select {
		case writeCh <- struct{}{}:
		default:
		}
	})

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Create a mock stream with invalid stream type
	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(3))
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()

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
}

func TestSession_ProcessBiStream_DecodeStreamTypeError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(5))
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

	assert.True(t, session.terminating(), "Session should be terminating after bi-stream decode error")
}

func TestSession_ProcessBiStream_DecodeAnnounceMessageError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(7)).Maybe()
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	mockStream.On("CancelRead", mock.Anything).Return().Maybe()

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

	mockStream.AssertExpectations(t)
}

func TestSession_ProcessBiStream_DecodeSubscribeMessageError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(StreamID(9)).Maybe()
	mockStream.On("Close").Return(nil).Maybe()
	mockStream.On("CancelWrite", mock.Anything).Return().Maybe()
	mockStream.On("CancelRead", mock.Anything).Return().Maybe()

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

	mockStream.AssertExpectations(t)
}

func TestSession_ProcessBiStream_Fetch(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	session := newTestSession(conn)

	called := false
	var gotReq *FetchRequest
	var gotWriter *GroupWriter
	session.fetchHandler = FetchHandlerFunc(func(w *GroupWriter, r *FetchRequest) {
		called = true
		gotReq = r
		gotWriter = w
	})

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Close").Return(nil).Maybe()

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

	mockStream.AssertExpectations(t)
	_ = session.CloseWithError(NoError, "")
}

func TestSession_Probe(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	requestStream := &MockQUICStream{}
	requestStream.On("Context").Return(context.Background())
	requestStream.On("CancelRead", mock.Anything).Return().Maybe()
	requestStream.On("CancelWrite", mock.Anything).Return().Maybe()
	requestStream.On("Close").Return(nil).Maybe()

	var written bytes.Buffer
	requestStream.WriteFunc = func(p []byte) (int, error) {
		return written.Write(p)
	}

	var response bytes.Buffer
	respMsg := message.ProbeMessage{Bitrate: 250000}
	require.NoError(t, respMsg.Encode(&response))
	requestStream.ReadFunc = response.Read

	conn.On("OpenStream").Return(requestStream, nil)

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	session := newTestSession(conn)

	probeStream := &MockQUICStream{}
	probeStream.On("Context").Return(context.Background())
	probeStream.On("CancelRead", mock.Anything).Return().Maybe()
	probeStream.On("CancelWrite", mock.Anything).Return().Maybe()
	probeStream.On("Close").Return(nil).Maybe()

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.ConnectionStatsFunc = func() quic.ConnectionStats {
		return quic.ConnectionStats{}
	}

	session := newTestSession(conn)

	probeStream := &MockQUICStream{}
	probeStream.On("Context").Return(context.Background())
	probeStream.On("CancelRead", mock.Anything).Return().Maybe()
	probeStream.On("CancelWrite", mock.Anything).Return().Maybe()
	probeStream.On("Close").Return(nil).Maybe()

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Add a track reader
	mockTrackStream := &MockQUICStream{}
	mockTrackStream.On("Context").Return(context.Background())
	mockTrackStream.On("Read", mock.Anything).Return(0, io.EOF).Maybe()
	mockTrackStream.On("Write", mock.Anything).Return(0, nil).Maybe()

	substr := newTestSendSubscribeStream(mockTrackStream, &SubscribeConfig{})
	trackReader := newTrackReader(testSubscribeRequest(t, nil), substr, func() {})
	session.addTrackReader(1, trackReader)

	// Create a mock receive stream for GROUP
	mockRecvStream := &MockQUICReceiveStream{}
	mockRecvStream.On("StreamID").Return(StreamID(4))
	mockRecvStream.On("CancelRead", mock.Anything).Return().Maybe()

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
	mockRecvStream.On("Context").Return(context.Background())

	session.processUniStream(mockRecvStream)

	// Verify group was enqueued
	time.Sleep(10 * time.Millisecond)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessUniStream_UnknownSubscribeID(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Create a mock receive stream for GROUP with unknown subscribe ID
	mockRecvStream := &MockQUICReceiveStream{}
	mockRecvStream.On("StreamID").Return(StreamID(5)).Maybe()
	mockRecvStream.On("CancelRead", StreamErrorCode(InvalidSubscribeIDErrorCode)).Return()

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

	mockRecvStream.AssertExpectations(t)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_ProcessUniStream_InvalidStreamType(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Create a mock receive stream with invalid stream type
	mockRecvStream := &MockQUICReceiveStream{}
	mockRecvStream.On("StreamID").Return(StreamID(6))
	mockRecvStream.On("CancelRead", mock.Anything).Return()

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	mockRecvStream := &MockQUICReceiveStream{}
	mockRecvStream.On("StreamID").Return(StreamID(11)).Maybe()
	mockRecvStream.ReadFunc = func(p []byte) (int, error) {
		return 0, io.ErrUnexpectedEOF
	}

	session.processUniStream(mockRecvStream)

	mockRecvStream.AssertExpectations(t)
}

func TestSession_ProcessUniStream_DecodeGroupMessageError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Maybe()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockSessStream := &MockQUICStream{}
	mockSessStream.On("Context").Return(context.Background())
	mockSessStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	mockRecvStream := &MockQUICReceiveStream{}
	mockRecvStream.On("StreamID").Return(StreamID(13)).Maybe()

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

	mockRecvStream.AssertExpectations(t)
}

func TestSession_Subscribe_TerminatingSession(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// Terminate the session
	err := session.CloseWithError(NoError, "")
	assert.NoError(t, err)

	// Wait for termination to complete
	time.Sleep(10 * time.Millisecond)

	// Try to subscribe - should fail because session is terminating
	config := &SubscribeConfig{Priority: 1}
	reader, err := session.Subscribe(context.Background(), testSubscribeRequest(t, config))

	// Subscribe should fail or return nil because session is terminated
	if err != nil || reader == nil {
		// Expected behavior
		assert.Nil(t, reader)
	}
}

func TestSession_AcceptAnnounce_TerminatingSession(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

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
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	appErr := &ApplicationError{
		ErrorCode:    ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}
	conn.On("OpenStream").Return(nil, appErr)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodeStreamTypeError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockAnnStream := &MockQUICStream{}
	mockAnnStream.On("StreamID").Return(StreamID(3))
	mockAnnStream.On("Context").Return(context.Background())
	mockAnnStream.On("Write", mock.Anything).Return(0, errors.New("write error"))

	conn.On("OpenStream").Return(mockAnnStream, nil)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodeStreamTypeStreamError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockAnnStream := &MockQUICStream{}
	mockAnnStream.On("StreamID").Return(StreamID(3))
	mockAnnStream.On("Context").Return(context.Background())
	mockAnnStream.On("CancelRead", mock.Anything).Return()

	strErr := &StreamError{
		ErrorCode: StreamErrorCode(InternalAnnounceErrorCode),
		Remote:    false,
	}
	mockAnnStream.On("Write", mock.Anything).Return(0, strErr)

	conn.On("OpenStream").Return(mockAnnStream, nil)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_EncodePleaseMessageStreamError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockAnnStream := &MockQUICStream{}
	mockAnnStream.On("StreamID").Return(StreamID(3))
	mockAnnStream.On("Context").Return(context.Background())
	mockAnnStream.On("CancelRead", mock.Anything).Return()

	// Use WriteFunc for direct control
	writeCallCount := 0
	strErr := &StreamError{
		ErrorCode: StreamErrorCode(InternalAnnounceErrorCode),
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

	conn.On("OpenStream").Return(mockAnnStream, nil)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.Error(t, err)
	assert.Nil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_DecodeInitMessageStreamError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockAnnStream := &MockQUICStream{}
	mockAnnStream.On("StreamID").Return(StreamID(3))
	mockAnnStream.On("Context").Return(context.Background())
	mockAnnStream.On("Write", mock.Anything).Return(0, nil)
	mockAnnStream.On("CancelRead", mock.Anything).Return()

	// Make Read fail with StreamError
	strErr := &StreamError{
		ErrorCode: StreamErrorCode(InternalAnnounceErrorCode),
		Remote:    false,
	}
	mockAnnStream.On("Read", mock.Anything).Return(0, strErr)

	conn.On("OpenStream").Return(mockAnnStream, nil)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_AcceptAnnounce_DecodeInitMessageError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockAnnStream := &MockQUICStream{}
	mockAnnStream.On("StreamID").Return(StreamID(3))
	mockAnnStream.On("Context").Return(context.Background())
	mockAnnStream.On("Write", mock.Anything).Return(0, nil)

	// Make Read fail with generic error
	mockAnnStream.On("Read", mock.Anything).Return(0, errors.New("read error"))

	conn.On("OpenStream").Return(mockAnnStream, nil)

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	reader, err := session.AcceptAnnounce("/test/prefix/")

	assert.NoError(t, err)
	assert.NotNil(t, reader)

	_ = session.CloseWithError(NoError, "")
}

func TestSession_CloseWithError_AlreadyTerminating(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil).Once()
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}).Maybe()
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	// First termination
	err1 := session.CloseWithError(NoError, "first termination")
	assert.NoError(t, err1)

	// Second termination should return immediately without error
	err2 := session.CloseWithError(InternalSessionErrorCode, "second termination")
	// The second call returns nil because terminating() is already true
	assert.NoError(t, err2)

	// Verify CloseWithError was only called once
	conn.AssertExpectations(t)
}

func TestSession_Terminate_WithApplicationError(t *testing.T) {
	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	appErr := &ApplicationError{
		ErrorCode:    ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(appErr)
	conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF).Maybe()
	conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF).Maybe()

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(context.Background())
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)

	session := newTestSession(conn)

	err := session.CloseWithError(InternalSessionErrorCode, "test error")

	assert.Error(t, err)
	var sessErr *SessionError
	assert.ErrorAs(t, err, &sessErr)
}
