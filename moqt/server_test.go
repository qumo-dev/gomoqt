package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type stubWTServer struct {
	serveErr error
	closed   bool
}

func (s *stubWTServer) ServeQUICConn(conn StreamConn) error { return s.serveErr }
func (s *stubWTServer) Close() error {
	s.closed = true
	return nil
}

func TestServer_Init(t *testing.T) {
	s := &Server{}
	s.init()

	assert.NotNil(t, s.listeners)
	assert.NotNil(t, s.sessionManager)
	assert.NotNil(t, s.WebTransportServer)
}

func TestServer_connContext_AppliesCustomAndInjectsServer(t *testing.T) {
	type customKey struct{}

	s := &Server{
		ConnContext: func(ctx context.Context, conn StreamConn) context.Context {
			return context.WithValue(ctx, customKey{}, "ok")
		},
	}
	s.init()

	ctx := s.connContext(context.Background(), &MockStreamConn{})

	assert.Equal(t, "ok", ctx.Value(customKey{}))
	ctxServer, ok := ctx.Value(serverContextKey).(*sessionManager)
	assert.True(t, ok)
	assert.Equal(t, s.sessionManager, ctxServer)
}

func TestServer_connContext_PanicsOnNilCustomContext(t *testing.T) {
	s := &Server{
		ConnContext: func(ctx context.Context, conn StreamConn) context.Context {
			return nil
		},
	}

	assert.Panics(t, func() {
		_ = s.connContext(context.Background(), &MockStreamConn{})
	})
}

func TestServer_ServeQUICListener_ShuttingDown(t *testing.T) {
	s := &Server{}
	s.inShutdown.Store(true)

	err := s.ServeQUICListener(&MockEarlyListener{})
	assert.Equal(t, ErrServerClosed, err)
}

func TestServer_ServeQUICConn_UnsupportedProtocol(t *testing.T) {
	s := &Server{}
	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: "unknown"})

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol")
}

func TestServer_ServeQUICConn_WebTransport(t *testing.T) {
	s := &Server{WebTransportServer: &stubWTServer{}}
	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: NextProtoH3})

	err := s.ServeQUICConn(conn)
	assert.NoError(t, err)
}

func TestServer_ServeQUICConn_NativeQUICCallsHandlerAndReturnsError(t *testing.T) {
	called := false
	s := &Server{
		Handler: HandleFunc(func(sess *Session) {
			called = true
		}),
	}

	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ})
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
	assert.True(t, called)
}

func TestServer_ServeQUICConn_NativeQUICWithoutHandlerReturnsError(t *testing.T) {
	s := &Server{}
	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ})

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
}

func TestServer_ListenAndServe_RequiresTLSConfig(t *testing.T) {
	s := &Server{}
	err := s.ListenAndServe()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration for TLS is required")
}

func TestServer_ListenAndServe_ConfiguresDefaultsBeforeListen(t *testing.T) {
	called := false
	var gotTLS *tls.Config
	var gotQUIC *quic.Config

	s := &Server{
		Addr:      "localhost:0",
		TLSConfig: &tls.Config{},
		ListenFunc: func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (QUICListener, error) {
			called = true
			gotTLS = tlsConfig
			gotQUIC = quicConfig
			return nil, errors.New("listen failed")
		},
	}

	err := s.ListenAndServe()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start QUIC listener")
	assert.True(t, called)
	assert.NotNil(t, gotTLS)
	assert.Equal(t, []string{NextProtoH3, NextProtoMOQ}, gotTLS.NextProtos)
	assert.NotNil(t, gotQUIC)
	assert.True(t, gotQUIC.EnableDatagrams)
	assert.True(t, gotQUIC.EnableStreamResetPartialDelivery)
	// Ensure original server TLS config was not modified in place.
	assert.Len(t, s.TLSConfig.NextProtos, 0)
}

func TestServer_ListenAndServeTLS_ShuttingDown(t *testing.T) {
	s := &Server{}
	s.inShutdown.Store(true)
	err := s.ListenAndServeTLS("cert.pem", "key.pem")
	assert.Equal(t, ErrServerClosed, err)
}

func TestServer_ListenAndServeTLS_InvalidKeyPair(t *testing.T) {
	s := &Server{Addr: "localhost:0"}
	err := s.ListenAndServeTLS("missing-cert.pem", "missing-key.pem")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load X509 key pair")
}

func TestServer_Close_ClosesListenersAndWTServer(t *testing.T) {
	s := &Server{WebTransportServer: &stubWTServer{}}
	s.init()

	ln := &MockEarlyListener{}
	ln.On("Close").Return(nil)
	s.listeners[ln] = struct{}{}

	err := s.Close()
	assert.NoError(t, err)
	assert.True(t, s.shuttingDown())
	ln.AssertCalled(t, "Close")
	assert.True(t, s.WebTransportServer.(*stubWTServer).closed)
}

func TestServer_Shutdown_NoSessions(t *testing.T) {
	s := &Server{}
	s.init()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Shutdown(ctx)
	assert.NoError(t, err)
	assert.True(t, s.shuttingDown())
}

func TestServer_addRemoveSession_ShutdownCompletesWhenLastSessionLeaves(t *testing.T) {
	s := &Server{}
	s.init()
	s.inShutdown.Store(true)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	sess := newSession(conn, nil, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	s.sessionManager.addSession(sess)
	done := s.sessionManager.Done()
	s.sessionManager.removeSession(sess)

	select {
	case <-done:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("doneChan should be closed when last session is removed during shutdown")
	}
}

func TestWebTransportHandler_upgradeWebTransport_WithoutServerContext(t *testing.T) {
	u := &WebTransportHandler{
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			conn := &MockWebTransportSession{}
			conn.On("Context").Return(context.Background())
			conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			return conn, nil
		},
	}
	r := &http.Request{TLS: &tls.ConnectionState{}}
	conn, err := u.upgradeWebTransport(&MockHTTPResponseWriter{}, r)
	require.NoError(t, err)
	require.NotNil(t, conn)
}

func TestWebTransportHandler_upgradeWebTransport_PlainHTTPRejected(t *testing.T) {
	u := &WebTransportHandler{}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r.TLS = nil
	r.RemoteAddr = "127.0.0.1:443"

	w := &MockHTTPResponseWriter{}
	w.On("Header").Return(make(http.Header)).Maybe()
	w.On("WriteHeader", http.StatusUpgradeRequired).Maybe()
	w.On("Write", mock.Anything).Return(0, nil).Maybe()

	_, err := u.upgradeWebTransport(w, r)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected CONNECT request")
}

func TestWebTransportHandler_upgradeWebTransport_UsesCustomUpgradeFunc(t *testing.T) {
	s := &Server{}
	s.init()

	u := &WebTransportHandler{
		TrackMux: NewTrackMux(),
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			conn := &MockWebTransportSession{}
			conn.On("Context").Return(context.Background())
			conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			conn.On("RemoteAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443})
			conn.On("LocalAddr").Return(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443})
			return conn, nil
		},
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r.TLS = &tls.ConnectionState{}

	w := &MockHTTPResponseWriter{}
	w.On("Header").Return(make(http.Header)).Maybe()

	conn, err := u.upgradeWebTransport(w, r)
	require.NoError(t, err)
	require.NotNil(t, conn)
}

func TestServer_handleNativeQUIC_NoHandlerConfigured(t *testing.T) {
	s := &Server{}
	err := s.handleNativeQUIC(&MockStreamConn{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
}

func TestServer_handleNativeQUIC_CallsHandlerAndReturnsError(t *testing.T) {
	called := false
	s := &Server{
		Handler: HandleFunc(func(sess *Session) {
			called = true
		}),
	}

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	err := s.handleNativeQUIC(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
	assert.True(t, called)
}
