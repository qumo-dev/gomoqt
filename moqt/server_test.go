package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	internalwt "github.com/okdaichi/gomoqt/moqt/internal/webtransportgo"
	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type stubWTServer struct {
	serveErr error
	closed   bool
}

func (s *stubWTServer) ServeQUICConn(conn transport.StreamConn) error { return s.serveErr }
func (s *stubWTServer) Close() error {
	s.closed = true
	return nil
}

func TestServer_Init(t *testing.T) {
	s := &Server{}
	s.init()

	assert.NotNil(t, s.listeners)
	assert.NotNil(t, s.activeSess)
	assert.NotNil(t, s.doneChan)
}

func TestServer_Init_ConfiguresDefaultWebTransportConnContext(t *testing.T) {
	s := &Server{}
	s.init()

	wt, ok := s.WebTransportServer.(*internalwt.Server)
	assert.True(t, ok)
	assert.NotNil(t, wt.ConnContext)
}

func TestServer_connContext_AppliesCustomAndInjectsServer(t *testing.T) {
	type customKey struct{}

	s := &Server{
		ConnContext: func(ctx context.Context, conn transport.StreamConn) context.Context {
			return context.WithValue(ctx, customKey{}, "ok")
		},
	}

	ctx := s.connContext(context.Background(), &MockStreamConn{})

	assert.Equal(t, "ok", ctx.Value(customKey{}))
	ctxServer, ok := ctx.Value(moqServerContextKey).(*Server)
	assert.True(t, ok)
	assert.Equal(t, s, ctxServer)
}

func TestServer_connContext_PanicsOnNilCustomContext(t *testing.T) {
	s := &Server{
		ConnContext: func(ctx context.Context, conn transport.StreamConn) context.Context {
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

func TestServer_ServeQUICConn_NativeQUICHandler(t *testing.T) {
	called := false
	s := &Server{
		NativeQUICHandler: &NativeQUICHandler{
			SessionHandler: func(sess *Session) error {
				called = true
				return nil
			},
		},
	}

	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ})
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	err := s.ServeQUICConn(conn)
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestServer_ServeQUICConn_NativeQUICWithoutHandler(t *testing.T) {
	s := &Server{}
	conn := &MockStreamConn{}
	conn.On("TLS").Return(&tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ})

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "native QUIC is not supported")
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
		ListenFunc: func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.QUICListener, error) {
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

func TestServer_addRemoveSession_ShutdownCompletes(t *testing.T) {
	s := &Server{}
	s.init()
	s.inShutdown.Store(true)

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	sess := newSession(conn, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	s.addSession(sess)
	s.removeSession(sess)

	select {
	case <-s.doneChan:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("doneChan should be closed when last session is removed during shutdown")
	}
}

func TestUpgrader_Upgrade_WithoutServerContext(t *testing.T) {
	u := &Upgrader{
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (transport.StreamConn, error) {
			conn := &MockStreamConn{}
			conn.On("Context").Return(context.Background())
			conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
			conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)
			return conn, nil
		},
	}
	r := &http.Request{TLS: &tls.ConnectionState{}}
	sess, err := u.Upgrade(&MockHTTPResponseWriter{}, r)
	assert.NoError(t, err)
	assert.NotNil(t, sess)
	_ = sess.CloseWithError(NoError, "")
}

func TestUpgrader_Upgrade_PlainHTTPRejected(t *testing.T) {
	s := &Server{}
	u := &Upgrader{}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r = r.WithContext(context.WithValue(context.Background(), moqServerContextKey, s))
	r.TLS = nil
	r.RemoteAddr = "127.0.0.1:443"

	w := &MockHTTPResponseWriter{}
	w.On("Header").Return(make(http.Header)).Maybe()
	w.On("WriteHeader", http.StatusUpgradeRequired).Maybe()
	w.On("Write", mock.Anything).Return(0, nil).Maybe()

	_, err := u.Upgrade(w, r)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plain HTTP")
}

func TestUpgrader_Upgrade_Success(t *testing.T) {
	s := &Server{}
	s.init()

	u := &Upgrader{
		TrackMux: NewTrackMux(),
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (transport.StreamConn, error) {
			conn := &MockStreamConn{}
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
	r = r.WithContext(context.WithValue(context.Background(), moqServerContextKey, s))
	r.TLS = &tls.ConnectionState{}

	w := &MockHTTPResponseWriter{}
	w.On("Header").Return(make(http.Header)).Maybe()

	sess, err := u.Upgrade(w, r)
	assert.NoError(t, err)
	assert.NotNil(t, sess)
	assert.Len(t, s.activeSess, 1)

	_ = sess.CloseWithError(NoError, "")
}

func TestNativeQUICHandler_NoSessionHandler(t *testing.T) {
	h := &NativeQUICHandler{}
	err := h.handleNativeQUIC(&MockStreamConn{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no session handler configured")
}

func TestNativeQUICHandler_WithSessionHandler(t *testing.T) {
	called := false
	h := &NativeQUICHandler{
		SessionHandler: func(sess *Session) error {
			called = true
			return errors.New("session handler error")
		},
	}

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	err := h.handleNativeQUIC(conn)
	assert.Error(t, err)
	assert.True(t, called)
}
