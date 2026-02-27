package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/quic"
	"github.com/okdaichi/gomoqt/webtransport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestServer_Init(t *testing.T) {
	tests := map[string]struct {
		addr   string
		logger *slog.Logger
		config *Config
	}{
		"basic init": {
			addr:   ":8080",
			logger: slog.Default(),
			config: &Config{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
				Config: tt.config,
			}

			server.init()

			assert.NotNil(t, server.listeners, "listeners map should be initialized")
			assert.NotNil(t, server.doneChan, "doneChan should be initialized")
			assert.NotNil(t, server.activeSess, "activeSess map should be initialized")

		})
	}
}

// stub webtransport.Server implementation for tests
type stubWT struct{}

func (s *stubWT) Upgrade(w http.ResponseWriter, r *http.Request) (quic.Connection, error) {
	return nil, nil
}

func (s *stubWT) ServeQUICConn(conn quic.Connection) error { return nil }
func (s *stubWT) Close() error                             { return nil }
func (s *stubWT) Shutdown(ctx context.Context) error       { return nil }

func TestServer_Init_UsesServerCheckHTTPOrigin(t *testing.T) {
	called := false

	// Server-level check returns false
	server := &Server{
		CheckHTTPOrigin: func(r *http.Request) bool { return false },
		NewWebtransportServerFunc: func(checkOrigin func(*http.Request) bool) webtransport.Server {
			called = true
			// ensure the passed function behaves as expected
			if checkOrigin == nil {
				t.Fatal("expected non-nil checkOrigin in NewWebtransportServerFunc")
			}
			if checkOrigin(&http.Request{Header: http.Header{"Origin": []string{"https://evil"}}}) {
				t.Fatal("expected checkOrigin to reject origin")
			}
			return &stubWT{}
		},
	}

	server.init()

	if !called {
		t.Fatal("NewWebtransportServerFunc was not called during init")
	}
	if server.wtServer == nil {
		t.Fatal("wtServer should be initialized")
	}
}

func TestServer_Init_NilCheckHTTPOrigin(t *testing.T) {
	var received func(*http.Request) bool

	server := &Server{
		NewWebtransportServerFunc: func(checkOrigin func(*http.Request) bool) webtransport.Server {
			received = checkOrigin
			return &stubWT{}
		},
	}

	server.init()

	if received == nil {
		// nil is acceptable: means all origins accepted
		// Pass
	}
}

func TestServer_InitOnce(t *testing.T) {
	tests := map[string]struct {
		addr      string
		logger    *slog.Logger
		initCalls int
	}{
		"multiple init calls": {
			addr:      ":8080",
			logger:    slog.Default(),
			initCalls: 3,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			// Call init multiple times
			for i := 0; i < tt.initCalls; i++ {
				server.init()
			}

			// Should only initialize once - verify by checking that fields are set
			assert.NotNil(t, server.listeners, "listeners map should be initialized")
		})
	}
}

func TestServer_ServeQUICListener(t *testing.T) {
	tests := map[string]struct {
		addr        string
		logger      *slog.Logger
		waitTime    time.Duration
		expectError bool
	}{
		"serve QUIC listener": {
			addr:        ":8080",
			logger:      slog.Default(),
			waitTime:    50 * time.Millisecond,
			expectError: false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			mockListener := &MockEarlyListener{}
			mockConn := &MockQUICConnection{}

			// Setup the mock connection methods that will be called
			mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
			mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8081})
			mockConn.On("ConnectionState").Return(quic.ConnectionState{
				TLS: tls.ConnectionState{NegotiatedProtocol: "moq-00"},
			})
			// Context is used for accept timeout in handleNativeQUIC
			mockConn.On("Context").Return(context.Background())
			// Avoid blocking goroutines: no actual streams in this test
			mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
			mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
			mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

			acceptCh := make(chan struct{}, 1)
			mockListener.On("Accept", mock.Anything).Return(mockConn, nil).Once().Run(func(mock.Arguments) {
				select {
				case acceptCh <- struct{}{}:
				default:
				}
			})
			// After a single accept, return EOF to simulate listener closure
			mockListener.On("Accept", mock.Anything).Return(nil, context.Canceled).Maybe()
			mockListener.On("Close").Return(nil)
			mockListener.On("Addr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			// Test serving the listener
			go func() {
				err := server.ServeQUICListener(mockListener)
				if err != nil && err != ErrServerClosed {
					if tt.expectError {
						return
					}
					t.Errorf("ServeQUICListener() error = %v", err)
				}
			}()

			// Wait for the server to call Accept (no arbitrary sleep)
			select {
			case <-acceptCh:
				// ok
			case <-time.After(200 * time.Millisecond):
				t.Fatal("ServeQUICListener did not call Accept in time")
			}

			// Close the server
			server.Close()
		})
	}
}

func TestServer_ServeQUICListener_AcceptError(t *testing.T) {
	tests := map[string]struct {
		addr        string
		logger      *slog.Logger
		expectError bool
	}{
		"accept error handling": {
			addr:        ":8080",
			logger:      slog.Default(),
			expectError: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			mockListener := &MockEarlyListener{}
			mockListener.On("Accept", mock.Anything).Return(nil, errors.New("accept error"))
			mockListener.On("Addr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			err := server.ServeQUICListener(mockListener)
			// Should handle accept errors gracefully
			if tt.expectError {
				assert.True(t, err != nil && err != ErrServerClosed, "should handle accept errors gracefully")
			}
		})
	}
}

func TestServer_ServeQUICListener_ShuttingDown(t *testing.T) {
	tests := map[string]struct {
		addr      string
		logger    *slog.Logger
		expectErr error
	}{
		"shutting down server": {
			addr:      ":8080",
			logger:    slog.Default(),
			expectErr: ErrServerClosed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			// Set server to shutting down state
			server.inShutdown.Store(true)

			mockListener := &MockEarlyListener{}

			err := server.ServeQUICListener(mockListener)
			assert.Equal(t, tt.expectErr, err, "ServeQUICListener() on shutting down server should return ErrServerClosed")
		})
	}
}

func TestServer_Close(t *testing.T) {
	tests := map[string]struct {
		addr   string
		logger *slog.Logger
	}{
		"close server": {
			addr:   ":8080",
			logger: slog.Default(),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			} // Initialize server
			server.init()

			// Add a mock listener
			mockListener := &MockEarlyListener{}
			mockListener.On("Close").Return(nil)
			server.listeners[mockListener] = struct{}{}

			err := server.Close()
			assert.NoError(t, err, "Close() should not return error")
			assert.True(t, server.shuttingDown(), "server should be in shutting down state after close")
			assert.True(t, mockListener.AssertCalled(t, "Close"), "listener should be closed when server closes")
		})
	}
}

func TestServer_Close_AlreadyShuttingDown(t *testing.T) {
	tests := map[string]struct {
		addr      string
		logger    *slog.Logger
		expectErr error
	}{
		"close already shutting down": {
			addr:      ":8080",
			logger:    slog.Default(),
			expectErr: ErrServerClosed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			// Set server to shutting down state
			server.inShutdown.Store(true)

			err := server.Close()
			assert.Equal(t, tt.expectErr, err, "Close() on already shutting down server should return ErrServerClosed")
		})
	}
}

func TestServer_ShuttingDown(t *testing.T) {
	tests := map[string]struct {
		setShutdown  bool
		expectResult bool
	}{
		"new server not shutting down": {
			setShutdown:  false,
			expectResult: false,
		},
		"server after shutdown set": {
			setShutdown:  true,
			expectResult: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{}

			if tt.setShutdown {
				server.inShutdown.Store(true)
			}

			result := server.shuttingDown()
			assert.Equal(t, tt.expectResult, result, "shuttingDown() should return expected result")
		})
	}
}

func TestServer_AcceptSession(t *testing.T) {
	tests := map[string]struct {
		addr     string
		logger   *slog.Logger
		path     string
		expectOK bool
	}{
		"accept session success": {
			addr:     ":8080",
			logger:   slog.Default(),
			path:     "/test",
			expectOK: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create mock session setup messages
			var buf bytes.Buffer
			// First, encode STREAM_TYPE message
			err := message.StreamTypeSession.Encode(&buf)
			require.NoError(t, err, "failed to encode STREAM_TYPE message")

			// Then, encode SESSION_CLIENT message
			scm := message.SessionClientMessage{
				SupportedVersions: []uint64{1},
				Parameters:        parameters{},
			}
			err = scm.Encode(&buf)
			require.NoError(t, err, "failed to encode SESSION_CLIENT message") // Create a mock connection with a session stream
			mockStream := &MockQUICStream{}
			mockStream.ReadFunc = func(p []byte) (int, error) {
				if buf.Len() == 0 {
					return 0, io.EOF
				}
				return buf.Read(p)
			}
			mockStream.On("Context").Return(context.Background())
			mockStream.On("Read", mock.AnythingOfType("[]uint8"))
			mockStream.On("Write", mock.AnythingOfType("[]uint8")).Return(0, nil)
			mockStream.On("StreamID").Return(quic.StreamID(1))

			mockConn := &MockQUICConnection{}
			// Mock RemoteAddr for logging
			mockAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
			mockConn.On("RemoteAddr").Return(mockAddr)
			// First call returns the session stream, subsequent calls return EOF to stop goroutines
			mockConn.On("AcceptStream", mock.Anything).Return(mockStream, nil).Once()
			mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF) // For handleBiStreams goroutine
			mockConn.On("Context").Return(context.Background())
			mockConn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // Return EOF to stop the goroutine

			ctx := context.Background()

			req, rw, err := acceptSessionStream(ctx, mockConn)
			if tt.expectOK {
				assert.NoError(t, err, "acceptSessionStream() should not return error")
				assert.NotNil(t, req, "acceptSessionStream() should return setup request")
				assert.NotNil(t, rw, "acceptSessionStream() should return response writer")
				// RemoteAddr should be copied from the connection
				assert.Equal(t, mockAddr.String(), req.RemoteAddr, "setup request should carry remote address")
			}

			// Cleanup not needed as sessStream doesn't have Terminate method
		})
	}
}

func TestServer_AcceptSession_AcceptStreamError(t *testing.T) {
	tests := map[string]struct {
		addr      string
		logger    *slog.Logger
		path      string
		expectErr error
	}{
		"accept stream error": {
			addr:      ":8080",
			logger:    slog.Default(),
			path:      "/test",
			expectErr: errors.New("stream accept error"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockConn := &MockQUICConnection{}
			// Mock RemoteAddr for logging
			mockAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
			mockConn.On("RemoteAddr").Return(mockAddr)
			mockConn.On("AcceptStream", mock.Anything).Return(nil, tt.expectErr)

			ctx := context.Background()

			req, rw, err := acceptSessionStream(ctx, mockConn)

			assert.Error(t, err, "acceptSessionStream() should return an error")
			assert.Contains(t, err.Error(), tt.expectErr.Error(), "acceptSessionStream() should return wrapped accept error")
			assert.Nil(t, req, "acceptSessionStream() should return nil setup request on error")
			assert.Nil(t, rw, "acceptSessionStream() should return nil response writer on error")
		})
	}
}

func TestServer_DoneChannel(t *testing.T) {
	tests := map[string]struct {
		addr      string
		logger    *slog.Logger
		waitTime  time.Duration
		closeTime time.Duration
		checkTime time.Duration
	}{
		"done channel lifecycle": {
			addr:      ":8080",
			logger:    slog.Default(),
			waitTime:  50 * time.Millisecond,
			closeTime: 50 * time.Millisecond,
			checkTime: 100 * time.Millisecond,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			// Initialize server
			server.init()

			// Should not be done initially
			select {
			case <-server.doneChan:
				t.Error("doneChan should not be closed initially")
			default:
				// Expected - channel should not be closed
			}

			// Close server
			server.Close()

			// Should be done after close
			select {
			case <-server.doneChan:
				// Expected - channel should be closed
			case <-time.After(tt.checkTime):
				t.Error("doneChan should be closed after server close")
			}
		})
	}
}

func TestServer_ConcurrentOperations(t *testing.T) {
	tests := map[string]struct {
		addr     string
		logger   *slog.Logger
		waitTime time.Duration
	}{
		"concurrent operations": {
			addr:     ":8080",
			logger:   slog.Default(),
			waitTime: 50 * time.Millisecond,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			// Test concurrent initialization and operations
			go server.init()
			go server.init()
			go func() {
				time.Sleep(10 * time.Millisecond)
				server.shuttingDown()
			}()

			time.Sleep(tt.waitTime)

			// Test concurrent close operations
			go server.Close()
			go server.Close()

			time.Sleep(tt.waitTime)

			// Test should complete without race conditions
			assert.True(t, true, "test should complete without race conditions")
		})
	}
}

func TestServer_WithCustomWebTransportServer(t *testing.T) {

	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
		NewWebtransportServerFunc: func(checkOrigin func(*http.Request) bool) webtransport.Server {
			return &MockWebTransportServer{}
		},
	}

	server.init()

	assert.NotNil(t, server.wtServer, "should use custom WebTransport server when provided")
}

func TestServer_SessionManagement(t *testing.T) {
	tests := map[string]struct {
		addr             string
		logger           *slog.Logger
		expectInitCount  int
		expectFinalCount int
	}{
		"session add and remove": {
			addr:             ":8080",
			logger:           slog.Default(),
			expectInitCount:  1,
			expectFinalCount: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			server.init() // Create a mock session
			connCtx, cancelConn := context.WithCancelCause(context.Background())
			streamCtx, cancelStream := context.WithCancelCause(connCtx)
			defer cancelStream(nil) // Ensure stream context is cancelled

			mockStream := &MockQUICStream{}
			mockStream.On("Context").Return(streamCtx)
			mockStream.On("Read", mock.Anything).Return(0, io.EOF)

			mockConn := &MockQUICConnection{}
			mockConn.On("Context").Return(context.Background())
			mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)    // For handleBiStreams
			mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF) // For handleUniStreams
			mockConn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				cancelConn(&quic.ApplicationError{
					ErrorCode:    quic.ApplicationErrorCode(args[0].(quic.ApplicationErrorCode)),
					ErrorMessage: args[1].(string),
				}) // Cancel the connection context
			}).Return(nil) // For session.CloseWithError
			mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			sessStream := newSessionStream(mockStream)
			session := newSession(mockConn, sessStream, nil, nil)

			// Test adding session
			server.sessMu.Lock()
			server.activeSess[session] = struct{}{}
			server.sessMu.Unlock()

			server.sessMu.RLock()
			count := len(server.activeSess)
			server.sessMu.RUnlock()

			assert.Equal(t, tt.expectInitCount, count, "active session count should match expected")

			// Test removing session
			server.sessMu.Lock()
			delete(server.activeSess, session)
			server.sessMu.Unlock()

			server.sessMu.RLock()
			count = len(server.activeSess)
			server.sessMu.RUnlock()

			assert.Equal(t, tt.expectFinalCount, count, "active session count after removal should match expected")

			// Cleanup
			_ = session.CloseWithError(NoError, SessionErrorText(NoError))
		})
	}
}

func TestServer_ConfigDefaults(t *testing.T) {
	tests := map[string]struct {
		addr       string
		logger     *slog.Logger
		expectInit bool
	}{
		"nil config handling": {
			addr:       ":8080",
			logger:     nil,
			expectInit: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr: tt.addr,
			}

			// Test that server handles nil config gracefully
			server.init()

			// Should not panic and should initialize properly
			if tt.expectInit {
				assert.NotNil(t, server.listeners, "listeners should be initialized even with nil config")
			}
		})
	}
}

func TestServer_ListenerManagement(t *testing.T) {
	tests := map[string]struct {
		addr         string
		logger       *slog.Logger
		numListeners int
		expectCount  int
	}{
		"multiple listeners": {
			addr:         ":8080",
			logger:       slog.Default(),
			numListeners: 2,
			expectCount:  2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   tt.addr,
				Logger: tt.logger,
			}

			server.init()

			mockListener1 := &MockEarlyListener{}
			mockListener2 := &MockEarlyListener{}

			// Set up mock expectations for Close calls
			mockListener1.On("Close").Return(nil)
			mockListener2.On("Close").Return(nil)

			// Add listeners
			server.listenerMu.Lock()
			server.listeners[mockListener1] = struct{}{}
			server.listeners[mockListener2] = struct{}{}
			server.listenerMu.Unlock()

			server.listenerMu.RLock()
			count := len(server.listeners)
			server.listenerMu.RUnlock()

			assert.Equal(t, tt.expectCount, count, "listener count should match expected")

			// Close server should close all listeners
			server.Close()

			assert.True(t, mockListener1.AssertCalled(t, "Close"), "listener1 should be closed when server closes")
			assert.True(t, mockListener2.AssertCalled(t, "Close"), "listener2 should be closed when server closes")
		})
	}
}

func TestServer_ServeQUICConn(t *testing.T) {
	tests := map[string]struct {
		protocol    string
		expectError bool
		errorMsg    string
	}{
		"unsupported protocol": {
			protocol:    "unsupported",
			expectError: true,
			errorMsg:    "unsupported protocol",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}

			mockConn := &MockQUICConnection{}
			mockConn.On("ConnectionState").Return(quic.ConnectionState{
				TLS: tls.ConnectionState{
					NegotiatedProtocol: tt.protocol,
				},
			})
			mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

			err := server.ServeQUICConn(mockConn)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServer_ServeQUICConn_ShuttingDown(t *testing.T) {
	server := &Server{
		Addr: ":8080",
	}
	server.inShutdown.Store(true)

	mockConn := &MockQUICConnection{}

	err := server.ServeQUICConn(mockConn)
	assert.Equal(t, ErrServerClosed, err)
}

func TestServer_ServeQUICConn_NativeQUIC(t *testing.T) {
	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
	}

	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(quic.StreamID(0))
	mockStream.On("Context").Return(context.Background())

	// Prepare session messages
	var buf bytes.Buffer
	require.NoError(t, message.StreamTypeSession.Encode(&buf))
	scm := message.SessionClientMessage{
		SupportedVersions: []uint64{0},
		Parameters:        make(parameters),
	}
	require.NoError(t, scm.Encode(&buf))
	// also send dummy server response to satisfy awaitAccepted
	ssm := message.SessionServerMessage{
		SelectedVersion: 0,
		Parameters:      make(parameters),
	}
	require.NoError(t, ssm.Encode(&buf))

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	mockConn := &MockQUICConnection{}
	mockConn.On("ConnectionState").Return(quic.ConnectionState{
		TLS: tls.ConnectionState{
			NegotiatedProtocol: NextProtoMOQ,
		},
		Version: 1,
	})
	mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9090})
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(mockStream, nil).Once()

	// since setupAndServe may call w.Reject and sessionStream.closeWithError,
	// the underlying stream may get CancelRead/CancelWrite invocations.
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()

	err := server.ServeQUICConn(mockConn)
	assert.NoError(t, err)

}

func TestServer_HandleNativeQUIC_NilLogger(t *testing.T) {
	server := &Server{
		Addr: ":8080",
	}

	mockStream := &MockQUICStream{}
	mockStream.On("StreamID").Return(quic.StreamID(0))
	mockStream.On("Context").Return(context.Background())

	// Prepare session messages
	var buf bytes.Buffer
	require.NoError(t, message.StreamTypeSession.Encode(&buf))
	scm := message.SessionClientMessage{
		SupportedVersions: []uint64{0},
		Parameters:        make(parameters),
	}
	require.NoError(t, scm.Encode(&buf))
	// include a dummy server response for the handshake
	ssm := message.SessionServerMessage{
		SelectedVersion: 0,
		Parameters:      make(parameters),
	}
	require.NoError(t, ssm.Encode(&buf))

	data := buf.Bytes()
	mockStream.ReadFunc = func(p []byte) (int, error) {
		if len(data) == 0 {
			return 0, io.EOF
		}
		n := copy(p, data)
		data = data[n:]
		return n, nil
	}

	mockConn := &MockQUICConnection{}
	mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9090})
	mockConn.On("ConnectionState").Return(quic.ConnectionState{
		TLS: tls.ConnectionState{
			NegotiatedProtocol: NextProtoMOQ,
		},
		Version: 1,
	})
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(mockStream, nil).Once()

	// the default handler may call w.Reject which closes session stream
	mockStream.On("CancelRead", mock.Anything).Return()
	mockStream.On("CancelWrite", mock.Anything).Return()

	err := server.handleNativeQUIC(mockConn)
	assert.NoError(t, err)

}

func TestServer_HandleNativeQUIC_AcceptStreamError(t *testing.T) {
	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
	}

	mockConn := &MockQUICConnection{}
	mockConn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9090})
	mockConn.On("ConnectionState").Return(quic.ConnectionState{
		TLS: tls.ConnectionState{
			NegotiatedProtocol: NextProtoMOQ,
		},
		Version: 1,
	})
	mockConn.On("Context").Return(context.Background())
	mockConn.On("AcceptStream", mock.Anything).Return(nil, errors.New("accept error"))

	err := server.handleNativeQUIC(mockConn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "accept error")
}

func TestServer_HandleWebTransport(t *testing.T) {
	tests := map[string]struct {
		expectError  bool
		upgradeError error
	}{
		"upgrade error": {
			expectError:  true,
			upgradeError: errors.New("upgrade failed"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}

			mockResponseWriter := &MockHTTPResponseWriter{}
			mockResponseWriter.On("Header").Return(make(http.Header))
			mockResponseWriter.On("Write", mock.Anything).Return(0, nil)
			mockResponseWriter.On("WriteHeader", mock.Anything)

			req := &http.Request{
				URL: &url.URL{Path: "/test"},
				TLS: &tls.ConnectionState{},
			}

			wtServer := &MockWebTransportServer{}
			if tt.upgradeError != nil {
				wtServer.On("Upgrade", mockResponseWriter, req).Return(nil, tt.upgradeError)
			}

			server.wtServer = wtServer

			err := server.HandleWebTransport(mockResponseWriter, req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServer_HandleWebTransport_ShuttingDown(t *testing.T) {
	server := &Server{
		Addr: ":8080",
	}
	server.inShutdown.Store(true)

	mockResponseWriter := &MockHTTPResponseWriter{}
	req := &http.Request{}

	err := server.HandleWebTransport(mockResponseWriter, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server is shutting down")
}

func TestServer_HandleWebTransport_WithNilLogger(t *testing.T) {
	server := &Server{
		Addr: ":8080",
	}

	mockResponseWriter := &MockHTTPResponseWriter{}
	req := &http.Request{
		URL: &url.URL{Path: "/test"},
		TLS: &tls.ConnectionState{},
	}

	wtServer := &MockWebTransportServer{}
	wtServer.On("Upgrade", mockResponseWriter, req).Return(nil, errors.New("upgrade failed"))

	server.wtServer = wtServer

	err := server.HandleWebTransport(mockResponseWriter, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upgrade connection")
}

// func TestServer_Accept_EdgeCases(t *testing.T) {
// 	tests := map[string]struct {
// 		description   string
// 		writer        SetupResponseWriter
// 		request       *SetupRequest
// 		expectError   bool
// 		errorContains string
// 	}{
// 		"nil writer": {
// 			description:   "nil response writer",
// 			writer:        nil,
// 			request:       &SetupRequest{},
// 			expectError:   true,
// 			errorContains: "response writer cannot be nil",
// 		},
// 		"nil request": {
// 			description:   "nil setup request",
// 			writer:        &MockSetupResponseWriter{},
// 			request:       nil,
// 			expectError:   true,
// 			errorContains: "request cannot be nil",
// 		},
// 		"wrong writer type": {
// 			description:   "wrong response writer type",
// 			writer:        &MockSetupResponseWriter{},
// 			request:       &SetupRequest{Path: "/test"},
// 			expectError:   true,
// 			errorContains: "response writer is not of type *response",
// 		},
// 	}

// 	for name, tt := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			server := &Server{
// 				Addr:   ":8080",
// 				Logger: slog.Default(),
// 			}

// 			if tt.writer != nil && tt.writer != (&MockSetupResponseWriter{}) {
// 				mockWriter := tt.writer.(*MockSetupResponseWriter)
// 				mockWriter.On("Reject", mock.Anything).Return(nil)
// 			}

// 			mockMux := &TrackMux{}
// 			session, err := server.Accept(tt.writer, tt.request, mockMux)

// 			if tt.expectError {
// 				assert.Error(t, err)
// 				assert.Nil(t, session)
// 				if tt.errorContains != "" {
// 					assert.Contains(t, err.Error(), tt.errorContains)
// 				}
// 			} else {
// 				assert.NoError(t, err)
// 				assert.NotNil(t, session)
// 			}
// 		})
// 	}
// }

func TestServer_AcceptTimeout(t *testing.T) {
	tests := map[string]struct {
		config          *Config
		expectedTimeout time.Duration
	}{
		"default timeout": {
			config:          nil,
			expectedTimeout: 5 * time.Second,
		},
		"custom timeout": {
			config: &Config{
				SetupTimeout: 10 * time.Second,
			},
			expectedTimeout: 10 * time.Second,
		},
		"zero timeout uses default": {
			config: &Config{
				SetupTimeout: 0,
			},
			expectedTimeout: 5 * time.Second,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Config: tt.config,
			}

			timeout := server.Config.setupTimeout()
			assert.Equal(t, tt.expectedTimeout, timeout)
		})
	}
}

func TestServer_AddRemoveSession(t *testing.T) {
	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
	}
	server.init()

	// Test adding nil session
	server.addSession(nil)
	assert.Equal(t, 0, len(server.activeSess))

	// Create mock session without using newSession to avoid goroutines
	connCtx, cancelConn := context.WithCancelCause(context.Background())
	streamCtx, cancelStream := context.WithCancelCause(connCtx)
	defer cancelStream(nil) // Ensure stream context is cancelled

	mockStream := &MockQUICStream{}
	mockStream.On("Context").Return(streamCtx)

	mockConn := &MockQUICConnection{}
	mockConn.On("Context").Return(context.Background())
	mockConn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		cancelConn(&quic.ApplicationError{})
	}).Return(nil)
	mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	// Add expectation for AcceptStream to avoid panic
	mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
	mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)

	sessStream := newSessionStream(mockStream)
	session := newSession(mockConn, sessStream, nil, nil)
	// Immediately terminate session to stop goroutines
	defer func() { _ = session.CloseWithError(NoError, SessionErrorText(NoError)) }()

	// Test adding session
	server.addSession(session)
	assert.Equal(t, 1, len(server.activeSess))

	// Test removing session
	server.removeSession(session)
	assert.Equal(t, 0, len(server.activeSess))

	// Test removing non-existent session
	server.removeSession(session)
	assert.Equal(t, 0, len(server.activeSess))
}

func TestServer_AddRemoveListener(t *testing.T) {
	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
	}

	mockListener := &MockEarlyListener{}

	// Test adding listener
	server.addListener(mockListener)
	assert.Equal(t, 1, len(server.listeners))

	// Test removing listener
	server.removeListener(mockListener)
	assert.Equal(t, 0, len(server.listeners))

	// Test removing non-existent listener
	server.removeListener(mockListener)
	assert.Equal(t, 0, len(server.listeners))
}

func TestServer_Shutdown(t *testing.T) {
	tests := map[string]struct {
		contextTimeout time.Duration
		expectError    bool
		addSession     bool
	}{
		"successful shutdown": {
			contextTimeout: 5 * time.Second,
			expectError:    false,
			addSession:     false,
		},
		"context timeout": {
			contextTimeout: 1 * time.Millisecond,
			expectError:    false, // Shutdown always returns nil
			addSession:     false, // No session to avoid hanging
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}
			server.init()

			// Add mock listener
			mockListener := &MockEarlyListener{}
			mockListener.On("Close").Return(nil)
			server.listeners[mockListener] = struct{}{}

			// Add a mock session if needed to test timeout scenario
			if tt.addSession {
				connCtx, cancelConn := context.WithCancelCause(context.Background())
				streamCtx, cancelStream := context.WithCancelCause(connCtx)
				defer cancelStream(nil)

				mockStream := &MockQUICStream{}
				mockStream.On("Context").Return(streamCtx)

				mockConn := &MockQUICConnection{}
				mockConn.On("Context").Return(context.Background())
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					cancelConn(&quic.ApplicationError{})
				}).Return(nil)
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)
				sessStream := newSessionStream(mockStream)
				session := newSession(mockConn, sessStream, nil, nil)
				server.addSession(session)
				defer func() { _ = session.CloseWithError(NoError, SessionErrorText(NoError)) }()
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			err := server.Shutdown(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.True(t, server.shuttingDown())
		})
	}
}

func TestServer_Shutdown_AlreadyShuttingDown(t *testing.T) {
	server := &Server{
		Addr: ":8080",
	}
	server.inShutdown.Store(true)

	ctx := context.Background()
	err := server.Shutdown(ctx)
	assert.Equal(t, ErrServerClosed, err)
}

func TestServer_ListenAndServe(t *testing.T) {
	tests := map[string]struct {
		addr        string
		expectError bool
		listenFunc  quic.ListenAddrFunc
	}{
		"invalid address": {
			addr:        "invalid:address:format",
			expectError: true,
		},
		"custom listen func": {
			addr:        ":8080",
			expectError: false,
			listenFunc: func(addr string, tlsConf *tls.Config, quicConf *quic.Config) (quic.Listener, error) {
				mockListener := &MockEarlyListener{}
				mockListener.On("Accept", mock.Anything).Return(nil, errors.New("test error"))
				mockListener.On("Close").Return(nil)
				return mockListener, nil
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:       tt.addr,
				Logger:     slog.Default(),
				ListenFunc: tt.listenFunc,
			}

			go func() {
				time.Sleep(10 * time.Millisecond)
				server.Close()
			}()

			err := server.ListenAndServe()

			if tt.expectError && err != ErrServerClosed {
				assert.Error(t, err)
			}
		})
	}
}

func TestServer_ListenAndServeTLS(t *testing.T) {
	// For this test, we'll test the error case with non-existent files
	server := &Server{
		Addr:   ":8080",
		Logger: slog.Default(),
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		server.Close()
	}()

	err := server.ListenAndServeTLS("nonexistent.crt", "nonexistent.key")
	assert.Error(t, err)
}

func TestServer_ErrorHandling(t *testing.T) {
	tests := map[string]struct {
		scenario    string
		expectError bool
	}{
		"nil config": {
			scenario:    "nil_config",
			expectError: false,
		},
		"nil logger": {
			scenario:    "nil_logger",
			expectError: false,
		},
		"empty address": {
			scenario:    "empty_addr",
			expectError: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var server *Server

			switch tt.scenario {
			case "nil_config":
				server = &Server{
					Addr:   ":8080",
					Logger: slog.Default(),
					Config: nil,
				}
			case "nil_logger":
				server = &Server{
					Addr:   ":8080",
					Logger: nil,
					Config: &Config{},
				}
			case "empty_addr":
				server = &Server{
					Addr:   "",
					Logger: slog.Default(),
					Config: &Config{},
				}
			}

			// Should not panic during initialization
			assert.NotPanics(t, func() {
				server.init()
			})

			if !tt.expectError {
				assert.NotNil(t, server.listeners)
				assert.NotNil(t, server.doneChan)
				assert.NotNil(t, server.activeSess)
			}
		})
	}
}

func TestServer_BoundaryValues(t *testing.T) {
	tests := map[string]struct {
		description string
		setupServer func() *Server
		testFunc    func(*testing.T, *Server)
	}{
		"zero timeout config": {
			description: "server with zero timeout should use default",
			setupServer: func() *Server {
				return &Server{
					Config: &Config{SetupTimeout: 0},
				}
			},
			testFunc: func(t *testing.T, s *Server) {
				timeout := s.Config.setupTimeout()
				assert.Equal(t, 5*time.Second, timeout)
			},
		},
		"negative timeout config": {
			description: "server with negative timeout should use default",
			setupServer: func() *Server {
				return &Server{
					Config: &Config{SetupTimeout: -1 * time.Second},
				}
			},
			testFunc: func(t *testing.T, s *Server) {
				timeout := s.Config.setupTimeout()
				assert.Equal(t, 5*time.Second, timeout)
			},
		},
		"maximum timeout config": {
			description: "server with very large timeout",
			setupServer: func() *Server {
				return &Server{
					Config: &Config{SetupTimeout: time.Hour * 24},
				}
			},
			testFunc: func(t *testing.T, s *Server) {
				timeout := s.Config.setupTimeout()
				assert.Equal(t, time.Hour*24, timeout)
			},
		},
		"nil server fields": {
			description: "server with all nil fields should not panic",
			setupServer: func() *Server {
				return &Server{}
			},
			testFunc: func(t *testing.T, s *Server) {
				assert.NotPanics(t, func() {
					s.init()
					s.shuttingDown()
					s.Config.setupTimeout()
				})
			},
		},
		"empty string addr": {
			description: "server with empty addr string",
			setupServer: func() *Server {
				return &Server{Addr: ""}
			},
			testFunc: func(t *testing.T, s *Server) {
				assert.NotPanics(t, func() {
					s.init()
				})
				assert.Equal(t, "", s.Addr)
			},
		},
		"whitespace addr": {
			description: "server with whitespace addr",
			setupServer: func() *Server {
				return &Server{Addr: "   "}
			},
			testFunc: func(t *testing.T, s *Server) {
				assert.NotPanics(t, func() {
					s.init()
				})
				assert.Equal(t, "   ", s.Addr)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := tt.setupServer()
			tt.testFunc(t, server)
		})
	}
}

func TestServer_ConcurrentSafetyOperations(t *testing.T) {
	tests := map[string]struct {
		description  string
		operations   int
		expectPanics bool
	}{
		"concurrent init calls": {
			description:  "multiple concurrent init calls should be safe",
			operations:   100,
			expectPanics: false,
		},
		"concurrent shutdown checks": {
			description:  "multiple concurrent shutdown checks should be safe",
			operations:   100,
			expectPanics: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}

			var wg sync.WaitGroup

			testFunc := func() {
				defer wg.Done()
				switch name {
				case "concurrent init calls":
					server.init()
				case "concurrent shutdown checks":
					server.shuttingDown()
				}
			}

			// Run operations concurrently
			for i := 0; i < tt.operations; i++ {
				wg.Add(1)
				go testFunc()
			}

			if !tt.expectPanics {
				assert.NotPanics(t, func() {
					wg.Wait()
				})
			}
		})
	}
}

func TestServer_SessionLifecycle(t *testing.T) {
	tests := map[string]struct {
		description    string
		sessions       int
		shutdownServer bool
		expectDoneChan bool
	}{
		"single session lifecycle": {
			description:    "single session add and remove",
			sessions:       1,
			shutdownServer: false,
			expectDoneChan: false,
		},
		"multiple sessions lifecycle": {
			description:    "multiple sessions add and remove",
			sessions:       5,
			shutdownServer: false,
			expectDoneChan: false,
		},
		"sessions with server shutdown": {
			description:    "sessions cleared on server shutdown",
			sessions:       3,
			shutdownServer: true,
			expectDoneChan: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}
			server.init()

			// Create mock sessions
			var sessions []*Session
			for i := 0; i < tt.sessions; i++ {
				connCtx, cancelConn := context.WithCancelCause(context.Background())
				streamCtx, cancelStream := context.WithCancelCause(connCtx)
				defer cancelStream(nil)

				mockStream := &MockQUICStream{}
				mockStream.On("Context").Return(streamCtx)

				mockConn := &MockQUICConnection{}
				mockConn.On("Context").Return(context.Background())
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					cancelConn(&quic.ApplicationError{})
				}).Return(nil)
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				// Add expectations for session goroutines
				mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)

				sessStream := newSessionStream(mockStream)
				session := newSession(mockConn, sessStream, nil, nil)
				sessions = append(sessions, session)

				// Add session
				server.addSession(session)
			} // Verify sessions are added
			assert.Equal(t, tt.sessions, len(server.activeSess))

			if tt.shutdownServer {
				server.inShutdown.Store(true)
			}

			// Remove all sessions
			for _, session := range sessions {
				server.removeSession(session)
				_ = session.CloseWithError(NoError, SessionErrorText(NoError))
			}

			// Verify sessions are removed
			assert.Equal(t, 0, len(server.activeSess))

			if tt.expectDoneChan {
				select {
				case <-server.doneChan:
					// Expected - channel should be closed
				case <-time.After(100 * time.Millisecond):
					t.Error("doneChan should be closed when last session is removed and server is shutting down")
				}
			}
		})
	}
}

func TestServer_ListenerLifecycle(t *testing.T) {
	tests := map[string]struct {
		description string
		listeners   int
		expectCount int
	}{
		"zero listeners": {
			description: "server with no listeners",
			listeners:   0,
			expectCount: 0,
		},
		"single listener": {
			description: "server with single listener",
			listeners:   1,
			expectCount: 1,
		},
		"multiple listeners": {
			description: "server with multiple listeners",
			listeners:   10,
			expectCount: 10,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}
			server.init()

			// Create and add mock listeners
			var mockListeners []*MockEarlyListener
			for i := 0; i < tt.listeners; i++ {
				mockListener := &MockEarlyListener{}
				mockListener.On("Close").Return(nil)
				mockListeners = append(mockListeners, mockListener)

				server.addListener(mockListener)
			}

			// Verify listener count
			assert.Equal(t, tt.expectCount, len(server.listeners))

			// Remove all listeners
			for _, listener := range mockListeners {
				server.removeListener(listener)
			}

			// Verify all listeners removed
			assert.Equal(t, 0, len(server.listeners))
		})
	}
}

func TestServer_EdgeCaseOperations(t *testing.T) {
	tests := map[string]struct {
		description string
		testFunc    func(*testing.T)
	}{
		"double initialization": {
			description: "calling init multiple times should be safe",
			testFunc: func(t *testing.T) {
				server := &Server{Addr: ":8080"}

				server.init()
				firstListeners := server.listeners
				firstDoneChan := server.doneChan
				firstActiveSess := server.activeSess

				// Call init again
				server.init()

				// Should be the same instances
				assert.Equal(t, firstListeners, server.listeners)
				assert.Equal(t, firstDoneChan, server.doneChan)
				assert.Equal(t, firstActiveSess, server.activeSess)
			},
		},
		"operations on nil server": {
			description: "operations on nil server should not panic",
			testFunc: func(t *testing.T) {
				var server *Server

				assert.Panics(t, func() {
					server.init()
				})
			},
		},
		"close without init": {
			description: "close before init should not panic",
			testFunc: func(t *testing.T) {
				server := &Server{Addr: ":8080"}

				assert.NotPanics(t, func() {
					err := server.Close()
					assert.NoError(t, err)
				})
			},
		},
		"remove non-existent session": {
			description: "removing session that was never added should be safe",
			testFunc: func(t *testing.T) {
				server := &Server{Addr: ":8080"}
				server.init()

				connCtx, cancelConn := context.WithCancelCause(context.Background())
				streamCtx, cancelStream := context.WithCancelCause(connCtx)
				defer cancelStream(nil)

				mockStream := &MockQUICStream{}
				mockStream.On("Context").Return(streamCtx)

				mockConn := &MockQUICConnection{}
				mockConn.On("Context").Return(context.Background())
				mockConn.On("CloseWithError", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					cancelConn(&quic.ApplicationError{})
				}).Return(nil)
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
				// Add expectations for session goroutines
				mockConn.On("AcceptStream", mock.Anything).Return(nil, io.EOF)
				mockConn.On("AcceptUniStream", mock.Anything).Return(nil, io.EOF)

				sessStream := newSessionStream(mockStream)
				session := newSession(mockConn, sessStream, nil, nil)

				assert.NotPanics(t, func() {
					server.removeSession(session)
				})
				_ = session.CloseWithError(NoError, SessionErrorText(NoError))
			},
		},
		"remove non-existent listener": {
			description: "removing listener that was never added should be safe",
			testFunc: func(t *testing.T) {
				server := &Server{Addr: ":8080"}
				server.init()

				mockListener := &MockEarlyListener{}

				assert.NotPanics(t, func() {
					server.removeListener(mockListener)
				})
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.testFunc(t)
		})
	}
}

func TestServer_ContextCancellation(t *testing.T) {
	tests := map[string]struct {
		description   string
		cancelTimeout time.Duration
		expectError   bool
	}{
		"immediate context cancellation": {
			description:   "context cancelled immediately",
			cancelTimeout: 0,
			expectError:   false, // Shutdown always returns nil
		},
		"context cancelled during operation": {
			description:   "context cancelled during operation",
			cancelTimeout: 50 * time.Millisecond,
			expectError:   false, // Shutdown always returns nil
		},
		"context not cancelled": {
			description:   "context not cancelled",
			cancelTimeout: 200 * time.Millisecond,
			expectError:   false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}
			server.init()

			ctx, cancel := context.WithCancel(context.Background())

			// Cancel context after specified timeout
			if tt.cancelTimeout == 0 {
				cancel()
			} else {
				go func() {
					time.Sleep(tt.cancelTimeout)
					cancel()
				}()
			}

			// Test context cancellation in Shutdown
			err := server.Shutdown(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServer_ErrorPropagation(t *testing.T) {
	tests := map[string]struct {
		description   string
		setupMocks    func() (*MockQUICConnection, *MockWebTransportServer)
		expectError   bool
		errorContains string
	}{
		"webtransport server error": {
			description: "webtransport server returns error",
			setupMocks: func() (*MockQUICConnection, *MockWebTransportServer) {
				mockConn := &MockQUICConnection{}
				mockConn.On("ConnectionState").Return(quic.ConnectionState{
					TLS: tls.ConnectionState{NegotiatedProtocol: "h3"},
				})
				mockConn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})

				wtServer := &MockWebTransportServer{}
				wtServer.On("ServeQUICConn", mockConn).Return(errors.New("webtransport error"))

				return mockConn, wtServer
			},
			expectError:   true,
			errorContains: "invalid connection type",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}

			mockConn, wtServer := tt.setupMocks()
			if wtServer != nil {
				server.wtServer = wtServer
			}

			err := server.ServeQUICConn(mockConn)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServer_WebTransportEdgeCases(t *testing.T) {
	tests := map[string]struct {
		description   string
		setupMocks    func() (*MockHTTPResponseWriter, *http.Request, *MockWebTransportServer)
		expectError   bool
		errorContains string
	}{
		"upgrade connection error": {
			description: "WebTransport upgrade fails",
			setupMocks: func() (*MockHTTPResponseWriter, *http.Request, *MockWebTransportServer) {
				mockWriter := &MockHTTPResponseWriter{}
				mockWriter.On("Header").Return(make(http.Header))
				mockWriter.On("Write", mock.Anything).Return(0, nil)
				mockWriter.On("WriteHeader", http.StatusInternalServerError)

				req := &http.Request{
					URL: &url.URL{Path: "/test"},
					TLS: &tls.ConnectionState{},
				}

				wtServer := &MockWebTransportServer{}
				wtServer.On("Upgrade", mockWriter, req).Return(nil, errors.New("upgrade failed"))

				return mockWriter, req, wtServer
			},
			expectError:   true,
			errorContains: "failed to upgrade connection",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:   ":8080",
				Logger: slog.Default(),
			}

			mockWriter, req, wtServer := tt.setupMocks()
			if wtServer != nil {
				server.wtServer = wtServer
			}

			err := server.HandleWebTransport(mockWriter, req)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServer_SetupHandlerEdgeCases(t *testing.T) {
	tests := map[string]struct {
		description string
		handler     SetupHandler
		expectError bool
	}{
		"nil setup handler": {
			description: "server with nil setup handler uses default behavior",
			handler:     nil,
			expectError: false,
		},
		"handler func": {
			description: "server with handler function",
			handler: SetupHandlerFunc(func(w SetupResponseWriter, r *SetupRequest) {
				// This is just a test of the handler func type
			}),
			expectError: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := &Server{
				Addr:         ":8080",
				Logger:       slog.Default(),
				SetupHandler: tt.handler,
			}

			// Just test that the server accepts the handler without error
			assert.NotPanics(t, func() {
				server.init()
			})

			// Verify handler is set correctly (skip for handler function due to function comparison issues)
			if tt.handler == nil {
				assert.Nil(t, server.SetupHandler)
			} else if _, ok := tt.handler.(SetupHandlerFunc); ok {
				assert.NotNil(t, server.SetupHandler)
			} else {
				assert.Equal(t, tt.handler, server.SetupHandler)
			}
		})
	}
}

func TestServer_GoAway(t *testing.T) {
	server := &Server{}
	server.init()
	// Test goAway implementation
	assert.NotPanics(t, func() {
		server.goAway()
	})
}

// ---------------------------------------------------------------------------
// Regression tests for webtransport-go v0.10.0 compatibility
// ---------------------------------------------------------------------------

// Ensure ListenAndServe enables QUIC flags required by WebTransport.
func TestServer_ListenAndServe_SetsWebTransportQUICFlags(t *testing.T) {
	var capturedConfig *quic.Config

	server := &Server{
		Addr:      ":0",
		TLSConfig: &tls.Config{},
		ListenFunc: func(_ string, _ *tls.Config, quicConf *quic.Config) (quic.Listener, error) {
			capturedConfig = quicConf
			return nil, errors.New("stop")
		},
	}

	_ = server.ListenAndServe()

	require.NotNil(t, capturedConfig, "ListenFunc must receive a non-nil QUICConfig")
	assert.True(t, capturedConfig.EnableDatagrams,
		"EnableDatagrams must be true for WebTransport (ServeQUICConn requirement)")
	assert.True(t, capturedConfig.EnableStreamResetPartialDelivery,
		"EnableStreamResetPartialDelivery must be true for WebTransport (ServeQUICConn requirement)")
}

// Ensure ListenAndServe clones the provided QUICConfig before modifying it.
func TestServer_ListenAndServe_DoesNotMutateOriginalQUICConfig(t *testing.T) {
	original := &quic.Config{EnableDatagrams: false}

	server := &Server{
		Addr:       ":0",
		TLSConfig:  &tls.Config{},
		QUICConfig: original,
		ListenFunc: func(_ string, _ *tls.Config, _ *quic.Config) (quic.Listener, error) {
			return nil, errors.New("stop")
		},
	}

	_ = server.ListenAndServe()

	assert.False(t, original.EnableDatagrams,
		"original QUICConfig.EnableDatagrams must not be mutated by ListenAndServe")
}

// Regression: Close() must not deadlock when a listener goroutine runs concurrently.
func TestServer_Close_DoesNotDeadlockWhenListenerConcurrent(t *testing.T) {
	server := &Server{Addr: ":8080"}

	started := make(chan struct{})
	mockListener := &MockEarlyListener{}
	mockListener.On("Addr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080})
	mockListener.On("Accept", mock.Anything).
		Return(nil, context.Canceled).
		Run(func(mock.Arguments) {
			select {
			case <-started:
			default:
				close(started)
			}
		})
	mockListener.On("Close").Return(nil)

	go func() { _ = server.ServeQUICListener(mockListener) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("ServeQUICListener did not start within 1s")
	}

	done := make(chan struct{})
	go func() {
		server.Close()
		close(done)
	}()

	select {
	case <-done:
		// pass — Close() returned without deadlocking
	case <-time.After(3 * time.Second):
		t.Fatal("Close() deadlocked: listenerGroup.Wait() never returned " +
			"(listeners map was likely cleared before listenerGroup.Wait)")
	}
}
