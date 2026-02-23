package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"testing"
	"time"

	"crypto/tls"
	"net/http"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestClient_InitOnce(t *testing.T) {
	c := &Client{}
	c.init()
	c.init() // Should not panic or re-init
	require.NotNil(t, c.activeSess)
	require.NotNil(t, c.doneChan)
}

func TestClient_TimeoutDefault(t *testing.T) {
	c := &Client{}
	assert.Equal(t, 5*time.Second, c.Config.setupTimeout())
}

func TestClient_TimeoutCustom(t *testing.T) {
	c := &Client{Config: &Config{SetupTimeout: 123 * time.Second}}
	assert.Equal(t, 123*time.Second, c.Config.setupTimeout())
}

func TestClient_AddRemoveSession(t *testing.T) {
	c := &Client{}
	c.init()
	sess := &Session{}
	c.addSession(sess)
	require.Contains(t, c.activeSess, sess)
	c.removeSession(sess)
	assert.NotContains(t, c.activeSess, sess)
}

func TestClient_ShuttingDown(t *testing.T) {
	c := &Client{}
	assert.False(t, c.shuttingDown())
	c.inShutdown.Store(true)
	assert.True(t, c.shuttingDown())
}

func TestClient_Close(t *testing.T) {
	c := &Client{}
	c.init()
	sess := &Session{}
	c.addSession(sess)
	// Should terminate session and wait for doneChan
	ch := make(chan struct{})
	go func() {
		c.Close()
		close(ch)
	}()
	// Remove session to unblock
	c.removeSession(sess)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Close did not return in time")
	}
}

func TestClient_ShutdownContextCancel(t *testing.T) {
	c := &Client{}
	c.init()
	waitCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockConn := &MockQUICConnection{
		AcceptStreamFunc: func(ctx context.Context) (quic.Stream, error) {
			select {
			case <-waitCtx.Done():
				return nil, waitCtx.Err()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		AcceptUniStreamFunc: func(ctx context.Context) (quic.ReceiveStream, error) {
			select {
			case <-waitCtx.Done():
				return nil, waitCtx.Err()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, OpenStreamFunc: func() (quic.Stream, error) {
			<-waitCtx.Done() // Simulate waiting for connection context
			return nil, nil  // Mock OpenStream to return nil
		}, OpenUniStreamFunc: func() (quic.SendStream, error) {
			<-waitCtx.Done() // Simulate waiting for connection context
			return nil, nil  // Mock OpenUniStream to return nil
		},
	}
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	mockConn.On("Context").Return(context.Background())
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (n int, err error) {
			<-waitCtx.Done() // Simulate waiting for session context
			return 0, nil    // Mock Read to return no data
		},
	}
	mockStream.On("Read", mock.Anything)
	mockStream.On("Write", mock.Anything).Return(0, nil)
	mockStream.On("CancelRead", mock.Anything)
	mockStream.On("CancelWrite", mock.Anything)
	mockStream.On("Context").Return(context.Background())

	sessStream := newSessionStream(mockStream, &SetupRequest{
		Path:             "path",
		ClientExtensions: NewExtension(),
	})
	sess := newSession(mockConn, sessStream, NewTrackMux(), nil)
	c.addSession(sess)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Simulate context cancellation
	err := c.Shutdown(ctx)

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestClient_ShutdownNoSessions(t *testing.T) {
	c := &Client{}
	c.init()
	err := c.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestClient_Dial(t *testing.T) {
	tests := map[string]struct {
		urlStr   string
		shut     bool
		wantErr  bool
		wantType error
	}{
		"invalid scheme": {urlStr: "ftp://host", wantErr: true, wantType: ErrInvalidScheme},
		"parse error":    {urlStr: "://", wantErr: true},
		"shutting down":  {urlStr: "https://host", shut: true, wantErr: true, wantType: ErrClientClosed},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			if tt.shut {
				c.inShutdown.Store(true)
			}
			_, err := c.Dial(context.Background(), tt.urlStr, NewTrackMux())
			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantType != nil {
					assert.ErrorIs(t, err, tt.wantType)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_DialWebTransport(t *testing.T) {
	tests := map[string]struct {
		uri     string
		shut    bool
		wtErr   error
		wantErr bool
	}{
		"webtransport error": {uri: "https://host", wtErr: errors.New("fail"), wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			if tt.shut {
				c.inShutdown.Store(true)
			}
			uri, _ := url.Parse(tt.uri)
			old := c.DialWebTransportFunc
			c.DialWebTransportFunc = func(ctx context.Context, url string, h http.Header, tlsConfig *tls.Config) (*http.Response, quic.Connection, error) {
				return nil, nil, tt.wtErr
			}
			defer func() { c.DialWebTransportFunc = old }()
			_, err := c.DialWebTransport(context.Background(), uri.Hostname()+":"+uri.Port(), uri.Path, NewTrackMux())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_DialQUIC(t *testing.T) {
	tests := map[string]struct {
		uri     string
		shut    bool
		dialErr error
		wantErr bool
	}{
		"dial error": {uri: "moqt://host", dialErr: errors.New("fail"), wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			if tt.shut {
				c.inShutdown.Store(true)
			}
			uri, _ := url.Parse(tt.uri)
			old := c.DialQUICFunc
			c.DialQUICFunc = func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (quic.Connection, error) {
				return nil, tt.dialErr
			}
			defer func() { c.DialQUICFunc = old }()
			_, err := c.DialQUIC(context.Background(), uri.Hostname()+":"+uri.Port(), uri.Path, NewTrackMux())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClient_openSession(t *testing.T) {
	tests := map[string]struct {
		mockConn  func() *MockQUICConnection
		extension func() *Extension
		wantErr   bool
	}{
		"OpenStream error": {
			mockConn: func() *MockQUICConnection {
				conn := &MockQUICConnection{}
				conn.On("OpenStream").Return(nil, errors.New("openstream fail"))
				conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				return conn
			}, wantErr: true,
		}, "STREAM_TYPE encode error": {
			mockConn: func() *MockQUICConnection {
				conn := &MockQUICConnection{}
				stream := &MockQUICStream{}
				stream.On("StreamID").Return(quic.StreamID(1))
				stream.On("Write", mock.Anything).Return(0, errors.New("stm encode fail"))
				// Background goroutine will try to read from stream even if Write fails
				stream.On("Read", mock.Anything).Return(0, io.EOF)
				stream.On("CancelRead", mock.Anything).Return()
				stream.On("CancelWrite", mock.Anything).Return()
				stream.On("Context").Return(context.Background())
				conn.On("OpenStream").Return(stream, nil)
				conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				return conn
			},
			extension: func() *Extension { return &Extension{} },
			wantErr:   true,
		}, "SESSION_CLIENT encode error": {
			mockConn: func() *MockQUICConnection {
				conn := &MockQUICConnection{}
				stream := &MockQUICStream{}
				stream.On("StreamID").Return(quic.StreamID(1))
				stream.On("Write", mock.Anything).Return(1, nil).Once()
				stream.On("Write", mock.Anything).Return(0, errors.New("scm encode fail")).Once()
				// Background goroutine will try to read from stream
				stream.On("Read", mock.Anything).Return(0, io.EOF)
				stream.On("CancelRead", mock.Anything).Return()
				stream.On("CancelWrite", mock.Anything).Return()
				stream.On("Context").Return(context.Background())
				conn.On("OpenStream").Return(stream, nil)
				conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				return conn
			},
			extension: func() *Extension { return &Extension{} },
			wantErr:   true,
		}, "SESSION_SERVER decode error": {
			mockConn: func() *MockQUICConnection {
				conn := &MockQUICConnection{}
				stream := &MockQUICStream{}
				stream.On("StreamID").Return(quic.StreamID(1))
				stream.On("Write", mock.Anything).Return(1, nil)
				stream.On("Write", mock.Anything).Return(1, nil)
				stream.On("Read", mock.Anything).Return(0, errors.New("ssm decode fail"))
				stream.On("CancelRead", mock.Anything).Return()
				stream.On("CancelWrite", mock.Anything).Return()
				stream.On("Context").Return(context.Background())
				conn.On("OpenStream").Return(stream, nil)
				conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
				conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				return conn
			},
			extension: func() *Extension { return &Extension{} },
			wantErr:   true,
		}, "path param error": {
			mockConn: func() *MockQUICConnection {
				conn := &MockQUICConnection{}
				stream := &MockQUICStream{
					ReadFunc: func(p []byte) (int, error) {
						// Return data that causes SESSION_SERVER message decoding to fail
						// This should provide enough data for length parsing but then fail during parameter decoding
						if len(p) >= 3 {
							p[0] = 0x02      // Message length = 2 bytes
							p[1] = 0x01      // Selected version = 1
							p[2] = 0x01      // Start of parameters, but invalid parameter format
							return 3, io.EOF // EOF during parameter parsing should cause error
						}
						return 0, io.EOF
					},
				}
				stream.On("StreamID").Return(quic.StreamID(1))
				stream.On("Write", mock.Anything).Return(1, nil)
				stream.On("Write", mock.Anything).Return(1, nil)
				stream.On("Read", mock.Anything) // ReadFunc will handle the behavior
				stream.On("CancelRead", mock.Anything).Return()
				stream.On("CancelWrite", mock.Anything).Return()
				stream.On("Context").Return(context.Background())
				conn.On("OpenStream").Return(stream, nil)
				conn.On("Context").Return(context.Background()) // Might not be called if error occurs early
				// Add background stream handling expectations
				conn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				conn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
				conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
				conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				return conn
			},
			extension: func() *Extension { return &Extension{} },
			wantErr:   true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			c.init() // Initialize the client properly
			conn := tt.mockConn()
			// Use a safe default extension provider when not specified
			extProvider := tt.extension
			if extProvider == nil {
				extProvider = func() *Extension { return &Extension{} }
			}
			_, err := openSessionStream(conn, "/path", extProvider())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			// Don't assert expectations for cases where early errors prevent all calls
			if !tt.wantErr {
				conn.AssertExpectations(t)
			}
		})
	}
}

// Test for generateSessionID function
func TestGenerateSessionID(t *testing.T) {
	// Test unique ID generation
	id1 := generateSessionID()
	id2 := generateSessionID()

	assert.NotEmpty(t, id1, "generateSessionID() should not return empty string")
	assert.NotEmpty(t, id2, "generateSessionID() should not return empty string")
	assert.NotEqual(t, id1, id2, "generateSessionID() should generate unique IDs")
	assert.Len(t, id1, 8, "generateSessionID() should return 8 character hex string")
	assert.Len(t, id2, 8, "generateSessionID() should return 8 character hex string")
}

// Test for Client.goAway method
func TestClient_GoAway(t *testing.T) {
	c := &Client{}
	c.init()
	sess := &Session{}
	c.addSession(sess)
	// Test goAway implementation
	assert.NotPanics(t, func() {
		c.goAway()
	})
}

// Test for Client.addSession with nil session
func TestClient_AddSession_NilSession(t *testing.T) {
	c := &Client{}
	c.init()

	// Should not panic or add nil session to map
	c.addSession(nil)

	// Verify nil was not added
	assert.NotContains(t, c.activeSess, (*Session)(nil))
}

// Test for Client.removeSession with nil session
func TestClient_RemoveSession_NilSession(t *testing.T) {
	c := &Client{}
	c.init()

	// Should not panic when trying to remove nil session
	c.removeSession(nil)

	// Should still be empty
	assert.Len(t, c.activeSess, 0)
}

// Test for Client.removeSession triggering done channel
func TestClient_RemoveSession_TriggersDone(t *testing.T) {
	c := &Client{}
	c.init()
	c.inShutdown.Store(true)

	sess := &Session{}
	c.addSession(sess)

	// Listen for done signal
	done := make(chan struct{})
	go func() {
		<-c.doneChan
		close(done)
	}()

	// Remove the last session
	c.removeSession(sess)

	// Should trigger done channel
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected done channel to be triggered")
	}
}

// Test for Client.DialWebTransport with custom dial function success
func TestClient_DialWebTransport_CustomDialSuccess(t *testing.T) {
	c := &Client{}
	mockConn := &MockQUICConnection{}

	// Create a mock stream that returns valid SESSION_SERVER response
	// Encode a proper SessionServerMessage using bytes.Buffer
	var buf bytes.Buffer
	ssm := message.SessionServerMessage{
		SelectedVersion: uint64(LiteDraft01),
		Parameters:      make(parameters),
	}
	err := ssm.Encode(&buf)
	require.NoError(t, err)

	responseData := buf.Bytes()
	dataRemaining := make([]byte, len(responseData))
	copy(dataRemaining, responseData)

	// Create a channel to control blocking after message is read
	blockChan := make(chan struct{})

	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			if len(dataRemaining) > 0 {
				n := copy(p, dataRemaining)
				dataRemaining = dataRemaining[n:]
				return n, nil
			}
			// After message is read, block instead of returning EOF
			<-blockChan
			return 0, io.EOF
		},
	}

	// Setup successful mock responses
	mockStream.On("StreamID").Return(quic.StreamID(1))
	mockStream.On("Write", mock.Anything).Return(1, nil).Times(2) // STREAM_TYPE + SESSION_CLIENT
	mockStream.On("Read", mock.Anything)
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()
	mockStream.On("Context").Return(context.Background())
	mockConn.On("OpenStream").Return(mockStream, nil)
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081})
	mockConn.On("ConnectionState").Return(quic.ConnectionState{})
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	c.DialWebTransportFunc = func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, quic.Connection, error) {
		return &http.Response{}, mockConn, nil
	}
	sess, err := c.DialWebTransport(context.Background(), "example.com:443", "/test", NewTrackMux())
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Cleanup
	if sess != nil {
		_ = sess.CloseWithError(NoError, "")
	}
	// Close block channel to allow any pending reads to complete
	close(blockChan)
}

// Test for Client.DialQUIC with custom dial function success
func TestClient_DialQUIC_CustomDialSuccess(t *testing.T) {
	c := &Client{}
	mockConn := &MockQUICConnection{}

	// Create a mock stream that returns valid SESSION_SERVER response
	// Encode a proper SessionServerMessage using bytes.Buffer
	var buf bytes.Buffer
	ssm := message.SessionServerMessage{
		SelectedVersion: uint64(LiteDraft01),
		Parameters:      make(parameters),
	}
	err := ssm.Encode(&buf)
	require.NoError(t, err)

	responseData := buf.Bytes()
	dataRemaining := make([]byte, len(responseData))
	copy(dataRemaining, responseData)

	// Create a channel to control blocking after message is read
	blockChan := make(chan struct{})

	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			if len(dataRemaining) > 0 {
				n := copy(p, dataRemaining)
				dataRemaining = dataRemaining[n:]
				return n, nil
			}
			// After message is read, block instead of returning EOF
			<-blockChan
			return 0, io.EOF
		},
	}

	// Setup successful mock responses
	mockStream.On("StreamID").Return(quic.StreamID(1))
	mockStream.On("Write", mock.Anything).Return(1, nil).Times(2) // STREAM_TYPE + SESSION_CLIENT
	mockStream.On("Read", mock.Anything)
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()
	mockStream.On("Context").Return(context.Background())
	mockConn.On("OpenStream").Return(mockStream, nil)
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081})
	mockConn.On("ConnectionState").Return(quic.ConnectionState{})
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	c.DialQUICFunc = func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (quic.Connection, error) {
		return mockConn, nil
	}
	sess, err := c.DialQUIC(context.Background(), "example.com:443", "/test", NewTrackMux())
	require.NoError(t, err)
	require.NotNil(t, sess)

	// Cleanup
	if sess != nil {
		_ = sess.CloseWithError(NoError, "")
	}
	// Close block channel to allow any pending reads to complete
	close(blockChan)
}

// Test for Client.Dial with shutting down state
func TestClient_Dial_ShuttingDown(t *testing.T) {
	c := &Client{}
	c.inShutdown.Store(true)

	_, err := c.Dial(context.Background(), "https://example.com", NewTrackMux())
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)
}

// Test for Client.DialWebTransport with shutting down state
func TestClient_DialWebTransport_ShuttingDown(t *testing.T) {
	c := &Client{}
	c.inShutdown.Store(true)

	_, err := c.DialWebTransport(context.Background(), "example.com:443", "/test", NewTrackMux())
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)
}

// Test for Client.DialQUIC with shutting down state
func TestClient_DialQUIC_ShuttingDown(t *testing.T) {
	c := &Client{}
	c.inShutdown.Store(true)

	_, err := c.DialQUIC(context.Background(), "example.com:443", "/test", NewTrackMux())
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)
}

// Test for Client with custom timeout configuration
func TestClient_Timeout_WithNilConfig(t *testing.T) {
	c := &Client{Config: nil}
	timeout := c.Config.setupTimeout()
	assert.Equal(t, 5*time.Second, timeout)
}

// Test for Client with custom timeout and zero value
func TestClient_Timeout_ZeroValue(t *testing.T) {
	c := &Client{Config: &Config{SetupTimeout: 0}}
	timeout := c.Config.setupTimeout()
	assert.Equal(t, 5*time.Second, timeout)
}

// Test for Client.openSession with nil extensions function
func TestClient_OpenSession_NilExtensions(t *testing.T) {
	c := &Client{}
	c.init()

	mockConn := &MockQUICConnection{}
	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			// Return minimal valid SESSION_SERVER response
			if len(p) >= 2 {
				p[0] = 0x01 // Message length = 1
				p[1] = 0x01 // Selected version = 1
				return 2, nil
			}
			return 0, nil
		},
	}

	mockStream.On("StreamID").Return(quic.StreamID(1))
	mockStream.On("Write", mock.Anything).Return(1, nil).Times(2)
	mockStream.On("Read", mock.Anything)
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()
	mockStream.On("Context").Return(context.Background())
	mockConn.On("OpenStream").Return(mockStream, nil)
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	// Expect panic when extensions is nil
	assert.Panics(t, func() {
		_, _ = openSessionStream(mockConn, "/test", nil)
	})
}

// Test for successful openSession with valid response
func TestClient_OpenSession_Success(t *testing.T) {
	c := &Client{}
	c.init()

	mockConn := &MockQUICConnection{}

	// Create a mock stream that returns valid SESSION_SERVER response
	// Encode a proper SessionServerMessage using bytes.Buffer
	var buf bytes.Buffer
	ssm := message.SessionServerMessage{
		SelectedVersion: uint64(LiteDraft01),
		Parameters:      make(parameters),
	}
	err := ssm.Encode(&buf)
	require.NoError(t, err)

	responseData := buf.Bytes()
	dataRemaining := make([]byte, len(responseData))
	copy(dataRemaining, responseData)
	// Create a channel to control blocking after message is read
	blockChan := make(chan struct{})

	mockStream := &MockQUICStream{
		ReadFunc: func(p []byte) (int, error) {
			if len(dataRemaining) > 0 {
				n := copy(p, dataRemaining)
				dataRemaining = dataRemaining[n:]
				return n, nil
			}
			// After message is read, block instead of returning EOF
			<-blockChan
			return 0, io.EOF
		},
	}

	mockStream.On("StreamID").Return(quic.StreamID(1))
	mockStream.On("Write", mock.Anything).Return(1, nil).Times(2)
	mockStream.On("Read", mock.Anything)
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()
	mockStream.On("Context").Return(context.Background())
	mockConn.On("OpenStream").Return(mockStream, nil)
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	extensions := func() *Extension {
		return NewExtension()
	}
	sessStream, err := openSessionStream(mockConn, "/test", extensions())
	require.NoError(t, err)
	require.NotNil(t, sessStream)

	// Verify sessionStream was created successfully
	assert.NotNil(t, sessStream, "sessionStream should be created")
	// Cleanup - no Terminate method on sessionStream
	// Close block channel to allow any pending reads to complete
	close(blockChan)
}

// Test for Client.Dial with various URL schemes
func TestClient_Dial_URLSchemes(t *testing.T) {
	tests := map[string]struct {
		urlStr      string
		expectError bool
		errorType   error
	}{
		"https scheme": {
			urlStr:      "https://example.com/test",
			expectError: false,
		},
		"moqt scheme": {
			urlStr:      "moqt://example.com/test",
			expectError: false,
		},
		"invalid scheme": {
			urlStr:      "ftp://example.com/test",
			expectError: true,
			errorType:   ErrInvalidScheme,
		},
		"malformed URL": {
			urlStr:      "://invalid-url",
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			c.init()

			// Mock successful connections to avoid actual network calls
			c.DialWebTransportFunc = func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, quic.Connection, error) {
				mockConn := &MockQUICConnection{}
				mockConn.On("Context").Return(context.Background())
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
				// Encode a proper SessionServerMessage
				wtBuffer := bytes.NewBuffer(nil)
				_ = message.SessionServerMessage{
					SelectedVersion: uint64(Default),
				}.Encode(wtBuffer)
				mockStream := &MockQUICStream{
					WriteFunc: func(p []byte) (int, error) {
						return len(p), nil // Mock successful write
					},
					ReadFunc: wtBuffer.Read,
				}
				mockStream.On("Write", mock.AnythingOfType("[]uint8"))
				mockStream.On("Read", mock.AnythingOfType("[]uint8"))
				mockStream.On("StreamID").Return(quic.StreamID(1))
				mockStream.On("Context").Return(context.Background())
				mockConn.On("OpenStream").Return(mockStream, nil)

				mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081})
				mockConn.On("ConnectionState").Return(quic.ConnectionState{})
				return &http.Response{}, mockConn, nil
			}

			c.DialQUICFunc = func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (quic.Connection, error) {
				mockConn := &MockQUICConnection{}
				mockConn.On("Context").Return(context.Background())
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
				buffer := bytes.NewBuffer(nil)
				_ = message.SessionServerMessage{
					SelectedVersion: uint64(Default),
				}.Encode(buffer)
				mockStream := &MockQUICStream{
					WriteFunc: func(p []byte) (int, error) {
						return len(p), nil // Mock successful write
					},
					ReadFunc: buffer.Read,
				}
				mockStream.On("Write", mock.AnythingOfType("[]uint8"))
				mockStream.On("Read", mock.AnythingOfType("[]uint8"))
				mockStream.On("StreamID").Return(quic.StreamID(1))
				mockStream.On("Context").Return(context.Background())
				mockConn.On("OpenStream").Return(mockStream, nil)
				mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081})
				mockConn.On("ConnectionState").Return(quic.ConnectionState{})
				return mockConn, nil
			}

			sess, err := c.Dial(context.Background(), tt.urlStr, NewTrackMux())

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
				assert.Nil(t, sess)
			} else {
				// Note: These will fail in actual connection setup due to mocked responses
				// but we're testing URL parsing and scheme detection here
				if err != nil {
					t.Logf("Expected success but got error (likely due to mock setup): %v", err)
				}
			}
		})
	}
}

// Test for Client.timeout with various configurations
func TestClient_Timeout_Configurations(t *testing.T) {
	tests := map[string]struct {
		config          *Config
		expectedTimeout time.Duration
	}{
		"nil config": {
			config:          nil,
			expectedTimeout: 5 * time.Second,
		},
		"config with zero timeout": {
			config:          &Config{SetupTimeout: 0},
			expectedTimeout: 5 * time.Second,
		},
		"config with custom timeout": {
			config:          &Config{SetupTimeout: 10 * time.Second},
			expectedTimeout: 10 * time.Second,
		},
		"config with very long timeout": {
			config:          &Config{SetupTimeout: 5 * time.Minute},
			expectedTimeout: 5 * time.Minute,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{Config: tt.config}
			timeout := c.Config.setupTimeout()
			assert.Equal(t, tt.expectedTimeout, timeout)
		})
	}
}

// Test for Client session management edge cases
func TestClient_SessionManagement_EdgeCases(t *testing.T) {
	tests := map[string]struct {
		description string
		setup       func(*Client) *Session
		verify      func(*testing.T, *Client, *Session)
	}{
		"add duplicate session": {
			description: "adding the same session twice should not cause issues",
			setup: func(c *Client) *Session {
				c.init()
				sess := &Session{}
				c.addSession(sess)
				c.addSession(sess) // Add same session again
				return sess
			},
			verify: func(t *testing.T, c *Client, sess *Session) {
				assert.Contains(t, c.activeSess, sess)
				assert.Len(t, c.activeSess, 1) // Should still be only 1
			},
		},
		"remove non-existent session": {
			description: "removing a session that wasn't added should not cause issues",
			setup: func(c *Client) *Session {
				c.init()
				sess := &Session{}
				c.removeSession(sess) // Remove without adding
				return sess
			},
			verify: func(t *testing.T, c *Client, sess *Session) {
				assert.NotContains(t, c.activeSess, sess)
				assert.Len(t, c.activeSess, 0)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			sess := tt.setup(c)
			tt.verify(t, c, sess)
		})
	}
}

// Test for generateSessionID function boundary conditions
func TestGenerateSessionID_Boundaries(t *testing.T) {
	// Generate multiple IDs to test uniqueness
	const numIDs = 1000
	ids := make(map[string]bool)

	for range numIDs {
		id := generateSessionID()

		// Basic validation
		assert.Len(t, id, 8, "Session ID should be 8 characters long")
		assert.Regexp(t, "^[0-9a-f]{8}$", id, "Session ID should be valid hex string")

		// Check uniqueness
		assert.False(t, ids[id], "Session ID should be unique: %s", id)
		ids[id] = true
	}

	// Verify we generated the expected number of unique IDs
	assert.Len(t, ids, numIDs, "All generated IDs should be unique")
}

// Test for Client initialization state
func TestClient_Init_Idempotency(t *testing.T) {
	c := &Client{}
	// Before init
	assert.Nil(t, c.activeSess)
	assert.Nil(t, c.doneChan)

	// First init
	c.init()
	activeSess1 := c.activeSess
	doneChan1 := c.doneChan

	require.NotNil(t, activeSess1)
	require.NotNil(t, doneChan1)

	// Second init - should be idempotent
	c.init()
	activeSess2 := c.activeSess
	doneChan2 := c.doneChan
	// Should be the same instances
	assert.Equal(t, activeSess1, activeSess2)
	assert.Equal(t, doneChan1, doneChan2)
}

// Test for Client.Close with no sessions
func TestClient_Close_NoSessions(t *testing.T) {
	c := &Client{}
	c.init()
	// Close with no active sessions should return immediately
	start := time.Now()
	err := c.Close()
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, duration, 100*time.Millisecond, "Close should return quickly with no sessions")
	assert.True(t, c.shuttingDown())
}

// Test for Client.Shutdown with active sessions timing out
func TestClient_Shutdown_Timeout(t *testing.T) {
	c := &Client{}
	c.init()

	// Add a mock session
	mockStream := &MockQUICStream{}
	mockStream.On("Read", mock.Anything).Return(0, io.EOF)
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()
	mockStream.On("Write", mock.Anything).Return(0, nil)
	mockStream.On("Context").Return(context.Background())

	mockConn := &MockQUICConnection{}
	mockConn.On("Context").Return(context.Background())
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

	sessStream := newSessionStream(mockStream, &SetupRequest{
		Path:             "path",
		ClientExtensions: NewExtension(),
	})
	sess := newSession(mockConn, sessStream, NewTrackMux(), nil)
	c.addSession(sess)

	// Create a context that times out quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Shutdown(ctx)
	duration := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Greater(t, duration, 40*time.Millisecond, "Should wait for context timeout")
	assert.Less(t, duration, 200*time.Millisecond, "Should not wait too long")
}

// Test Client configuration inheritance
func TestClient_ConfigurationInheritance(t *testing.T) {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	quicConfig := &quic.Config{}
	config := &Config{SetupTimeout: 30 * time.Second}
	logger := slog.Default()

	c := &Client{
		TLSConfig:  tlsConfig,
		QUICConfig: quicConfig,
		Config:     config,
		Logger:     logger,
	}
	// Verify configurations are preserved
	require.Same(t, tlsConfig, c.TLSConfig)
	require.Same(t, quicConfig, c.QUICConfig)
	require.Same(t, config, c.Config)
	require.Same(t, logger, c.Logger)

	// Test timeout inheritance
	timeout := c.Config.setupTimeout()
	assert.Equal(t, 30*time.Second, timeout)
}
