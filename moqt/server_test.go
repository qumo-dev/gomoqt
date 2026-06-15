package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestNativeQUICConn(tb testing.TB, opts ...func(*FakeStreamConn)) *FakeStreamConn {
	tb.Helper()
	conn := &FakeStreamConn{}
	conn.TLSFunc = func() *tls.ConnectionState {
		return &tls.ConnectionState{NegotiatedProtocol: NextProtoMOQ}
	}
	for _, opt := range opts {
		opt(conn)
	}
	return conn
}

func TestServer_Init(t *testing.T) {
	s := &Server{}
	s.init()

	assert.NotNil(t, s.listeners)
	assert.NotNil(t, s.connManager)
	assert.NotNil(t, s.WebTransportServer)
}

// TestServer_defaultWebTransportHandler_WiresServerFields guards against the
// regression where Server.init() passed a nil HTTP handler to
// NewWebTransportServer, causing every WebTransport request to 404. The default
// handler must now carry the Server's Handler so upgraded sessions dispatch.
func TestServer_defaultWebTransportHandler_WiresServerFields(t *testing.T) {
	h := HandleFunc(func(*Session) {})
	cfg := &Config{}
	mux := NewTrackMux(0)

	s := &Server{Config: cfg, TrackMux: mux, Handler: h}
	got := s.defaultWebTransportHandler()

	wth, ok := got.(*WebTransportHandler)
	require.True(t, ok, "default handler must be a *WebTransportHandler")
	assert.NotNil(t, wth.Handler, "Handler must be wired (was nil -> 404 before the fix)")
	_, isHandleFunc := wth.Handler.(HandleFunc)
	assert.True(t, isHandleFunc, "Handler must be the Server's HandleFunc")
	assert.Equal(t, cfg, wth.Config)
	assert.Equal(t, mux, wth.TrackMux)
}

// TestWebTransportHandler_ServeHTTP_NilHandlerFallsBack ensures a
// WebTransportHandler with no configured Handler does not panic (the previous
// path called u.Handler.ServeMOQ unconditionally) and instead falls back.
func TestWebTransportHandler_ServeHTTP_NilHandlerFallsBack(t *testing.T) {
	u := &WebTransportHandler{} // Handler and FallbackHandler are both nil

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "/broadcast", nil)

	require.NotPanics(t, func() {
		u.ServeHTTP(rr, req)
	})

	// With no Handler and no FallbackHandler, the fallback returns 400.
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestServer_WebTransportDial_UpgradesSession is an end-to-end regression
// test. Before the fix, Server.init() wired a nil HTTP handler, so a
// WebTransport dial to the server 404'd (see BenchmarkStreamIo_RealQUIC's
// b.Skipf and BenchmarkBroadcastServer_HighLoad reporting 0 frames/op).
// After the fix, the default WebTransportHandler upgrades the request and
// dispatches the upgraded session to the Server's Handler.
func TestServer_WebTransportDial_UpgradesSession(t *testing.T) {
	// Grab a free UDP port for the QUIC listener.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var handlerCalled atomic.Bool
	server := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{NextProtoH3, NextProtoMOQ},
			Certificates:       []tls.Certificate{generateTestCert(t)},
			InsecureSkipVerify: true,
		},
		Handler: HandleFunc(func(sess *Session) {
			handlerCalled.Store(true)
			<-sess.Context().Done()
		}),
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			t.Logf("server ListenAndServe: %v", err)
		}
	}()
	// Close the server with a bounded wait. Server.Close() blocks on
	// <-connManager.Done(), and a peer connection closed immediately before
	// Close is not always removed from the connManager in time, so an
	// unbounded Close would hang the test. That drain race is tracked
	// separately; here we just keep the test from hanging.
	defer func() {
		done := make(chan struct{})
		go func() { _ = server.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}()

	// Allow the QUIC listener to bind.
	time.Sleep(100 * time.Millisecond)

	client := &Dialer{
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}

	sess, err := client.Dial(ctx, "https://"+addr+"/moqt", nil)
	require.NoError(t, err, "dial should upgrade to a MOQ session, not 404 (regression)")
	defer sess.CloseWithError(NoError, "test done")

	require.Eventually(t, handlerCalled.Load, time.Second, 10*time.Millisecond,
		"server Handler must be invoked for the upgraded session")
}

func TestServer_connContext_AppliesCustomAndInjectsServer(t *testing.T) {
	type customKey struct{}

	s := &Server{
		ConnContext: func(ctx context.Context, conn StreamConn) context.Context {
			return context.WithValue(ctx, customKey{}, "ok")
		},
	}
	s.init()

	ctx := s.connContext(context.Background(), &FakeStreamConn{})

	assert.Equal(t, "ok", ctx.Value(customKey{}))
	ctxServer, ok := ctx.Value(serverContextKey).(*connManager)
	assert.True(t, ok)
	assert.Equal(t, s.connManager, ctxServer)
}

func TestServer_connContext_PanicsOnNilCustomContext(t *testing.T) {
	s := &Server{
		ConnContext: func(ctx context.Context, conn StreamConn) context.Context {
			return nil
		},
	}

	assert.Panics(t, func() {
		_ = s.connContext(context.Background(), &FakeStreamConn{})
	})
}

func TestServer_ServeQUICListener_ShuttingDown(t *testing.T) {
	s := &Server{}
	s.inShutdown.Store(true)

	err := s.ServeQUICListener(&FakeEarlyListener{})
	assert.Equal(t, ErrServerClosed, err)
}

func TestServer_ServeQUICConn_UnsupportedProtocol(t *testing.T) {
	s := &Server{}
	conn := &FakeStreamConn{}
	conn.TLSFunc = func() *tls.ConnectionState {
		return &tls.ConnectionState{NegotiatedProtocol: "unknown"}
	}

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol")
}

func TestServer_ServeQUICConn_WebTransport(t *testing.T) {
	s := &Server{WebTransportServer: &FakeWebTransportServer{}}
	conn := &FakeStreamConn{}
	conn.TLSFunc = func() *tls.ConnectionState {
		return &tls.ConnectionState{NegotiatedProtocol: NextProtoH3}
	}

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

	conn := newTestNativeQUICConn(t)

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
	assert.True(t, called)
}

func TestServer_ServeQUICConn_NativeQUICWithoutHandlerReturnsError(t *testing.T) {
	s := &Server{}
	conn := newTestNativeQUICConn(t)

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
	closed := false
	s := &Server{WebTransportServer: &FakeWebTransportServer{
		CloseFunc: func() error {
			closed = true
			return nil
		},
	}}
	s.init()

	ln := &FakeEarlyListener{}
	s.listeners[ln] = struct{}{}

	err := s.Close()
	assert.NoError(t, err)
	assert.True(t, s.shuttingDown())
	assert.True(t, ln.closed)
	assert.True(t, closed)
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

	conn := &FakeStreamConn{}

	sess := newSession(conn, nil, nil, nil, nil, nil, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	s.connManager.addConn(conn)
	done := s.connManager.Done()
	s.connManager.removeConn(conn)

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
			conn := &FakeWebTransportSession{}
			return conn, nil
		},
	}
	r := &http.Request{TLS: &tls.ConnectionState{}}
	conn, err := u.upgradeWebTransport(&FakeHTTPResponseWriter{}, r)
	require.NoError(t, err)
	require.NotNil(t, conn)
}

func TestWebTransportHandler_upgradeWebTransport_PlainHTTPRejected(t *testing.T) {
	u := &WebTransportHandler{}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r.TLS = nil
	r.RemoteAddr = "127.0.0.1:443"

	w := &FakeHTTPResponseWriter{}

	_, err := u.upgradeWebTransport(w, r)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected CONNECT request")
}

func TestWebTransportHandler_upgradeWebTransport_UsesCustomUpgradeFunc(t *testing.T) {
	s := &Server{}
	s.init()

	u := &WebTransportHandler{
		TrackMux: NewTrackMux(0),
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			conn := &FakeWebTransportSession{}
			conn.RemoteAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443} }
			conn.LocalAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443} }
			return conn, nil
		},
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r.TLS = &tls.ConnectionState{}

	w := &FakeHTTPResponseWriter{}

	conn, err := u.upgradeWebTransport(w, r)
	require.NoError(t, err)
	require.NotNil(t, conn)
}

func TestServer_handleNativeQUIC_NoHandlerConfigured(t *testing.T) {
	s := &Server{}
	err := s.handleNativeQUIC(&FakeStreamConn{})
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

	conn := &FakeStreamConn{}

	err := s.handleNativeQUIC(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no native QUIC handler configured")
	assert.True(t, called)
}

func TestListenAndServe_PackageLevel(t *testing.T) {
	err := ListenAndServe("", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration for TLS is required")
}

func TestStreamConnContext_Context(t *testing.T) {
	ctx := context.WithValue(context.Background(), serverContextKey, "test-value")
	inner := &FakeStreamConn{}
	w := &streamConnContext{StreamConn: inner, ctx: ctx}

	assert.Equal(t, ctx, w.Context())
	assert.Equal(t, "test-value", w.Context().Value(serverContextKey))
}

func TestStreamConnContext_QUICConn_NoProvider(t *testing.T) {
	inner := &FakeStreamConn{}
	w := &streamConnContext{StreamConn: inner, ctx: context.Background()}

	assert.Nil(t, w.QUICConn())
}

func TestWebTransportHandler_ServeHTTP_UpgradeSuccess(t *testing.T) {
	handlerCalled := false
	u := &WebTransportHandler{
		TrackMux: NewTrackMux(0),
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			sess := &FakeWebTransportSession{}
			sess.RemoteAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443} }
			sess.LocalAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443} }
			return sess, nil
		},
		Handler: HandleFunc(func(sess *Session) {
			handlerCalled = true
		}),
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	r.TLS = &tls.ConnectionState{}
	w := &FakeHTTPResponseWriter{}

	u.ServeHTTP(w, r)
	assert.True(t, handlerCalled)
}

func TestWebTransportHandler_ServeHTTP_UpgradeSuccessWithConnManager(t *testing.T) {
	s := &Server{}
	s.init()

	handlerCalled := false
	u := &WebTransportHandler{
		TrackMux: NewTrackMux(0),
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			sess := &FakeWebTransportSession{}
			sess.RemoteAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443} }
			sess.LocalAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443} }
			return sess, nil
		},
		Handler: HandleFunc(func(sess *Session) {
			handlerCalled = true
		}),
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	ctx := context.WithValue(r.Context(), serverContextKey, s.connManager)
	r = r.WithContext(ctx)
	r.TLS = &tls.ConnectionState{}
	w := &FakeHTTPResponseWriter{}

	u.ServeHTTP(w, r)
	assert.True(t, handlerCalled)
}

func TestWebTransportHandler_ServeHTTP_UpgradeFailFallback(t *testing.T) {
	u := &WebTransportHandler{
		UpgradeFunc: func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
			return nil, errors.New("upgrade failed")
		},
		FallbackHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	w := &FakeHTTPResponseWriter{}

	u.ServeHTTP(w, r)
}

func TestWebTransportHandler_Fallback_WithHandler(t *testing.T) {
	called := false
	h := &WebTransportHandler{
		FallbackHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	w := &FakeHTTPResponseWriter{}

	h.fallback(w, r)
	assert.True(t, called)
}

func TestWebTransportHandler_Fallback_NoHandler(t *testing.T) {
	h := &WebTransportHandler{}

	r, _ := http.NewRequest(http.MethodGet, "https://example.com/moq", nil)
	w := &FakeHTTPResponseWriter{}

	h.fallback(w, r)
}

func TestServer_ServeQUICListener_AcceptsAndServesConn(t *testing.T) {
	served := make(chan struct{})
	s := &Server{
		WebTransportServer: &FakeWebTransportServer{
			ServeQUICConnFunc: func(conn StreamConn) error {
				close(served)
				return nil
			},
		},
	}

	conn := &FakeStreamConn{}
	conn.TLSFunc = func() *tls.ConnectionState {
		return &tls.ConnectionState{NegotiatedProtocol: NextProtoH3}
	}

	accepted := false
	ln := &FakeEarlyListener{
		AcceptFunc: func(ctx context.Context) (StreamConn, error) {
			if !accepted {
				accepted = true
				return conn, nil
			}
			// Block until context is cancelled (server closing)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ServeQUICListener(ln)
	}()

	// Wait for the connection to be served
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connection to be served")
	}

	// Shut down the server to stop the listener loop
	s.inShutdown.Store(true)
	ln.Close()

	select {
	case err := <-errCh:
		assert.Equal(t, ErrServerClosed, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ServeQUICListener to return")
	}
}

func TestServer_ServeQUICListener_AcceptError(t *testing.T) {
	s := &Server{}
	ln := &FakeEarlyListener{
		AcceptFunc: func(ctx context.Context) (StreamConn, error) {
			return nil, errors.New("accept failed")
		},
	}

	err := s.ServeQUICListener(ln)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to accept QUIC connection")
}

func TestServer_ServeQUICConn_NilTLS(t *testing.T) {
	s := &Server{}
	conn := &FakeStreamConn{} // TLS returns nil by default

	err := s.ServeQUICConn(conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not have TLS information")
}

func TestServer_goAway_SendsGoawayMessage(t *testing.T) {
	var written []byte
	stream := &FakeQUICStream{
		WriteFunc: func(p []byte) (int, error) {
			written = append(written, p...)
			return len(p), nil
		},
	}

	connCtx, connCancel := context.WithCancel(context.Background())
	conn := &FakeStreamConn{
		OpenStreamFunc: func() (transport.Stream, error) {
			return stream, nil
		},
		ParentCtx: connCtx,
	}

	// Cancel the connection context to simulate connection close
	connCancel()

	s := &Server{NextSessionURI: "https://new-server.example.com"}
	err := s.goAway(context.Background(), conn)
	assert.NoError(t, err)
	assert.NotEmpty(t, written)
}

func TestServer_goAway_OpenStreamError(t *testing.T) {
	conn := &FakeStreamConn{
		OpenStreamFunc: func() (transport.Stream, error) {
			return nil, errors.New("stream error")
		},
	}

	s := &Server{}
	err := s.goAway(context.Background(), conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream error")
}

func TestServer_goAway_ContextCanceled(t *testing.T) {
	stream := &FakeQUICStream{
		WriteFunc: func(p []byte) (int, error) {
			return len(p), nil
		},
	}

	conn := &FakeStreamConn{
		OpenStreamFunc: func() (transport.Stream, error) {
			return stream, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	s := &Server{}
	err := s.goAway(ctx, conn)
	assert.NoError(t, err)
}

func TestServer_addListener_removeListener(t *testing.T) {
	s := &Server{}
	s.init()

	ln := &FakeEarlyListener{}
	s.addListener(ln)

	s.listenerMu.RLock()
	_, ok := s.listeners[ln]
	s.listenerMu.RUnlock()
	assert.True(t, ok)

	s.removeListener(ln)

	s.listenerMu.RLock()
	_, ok = s.listeners[ln]
	s.listenerMu.RUnlock()
	assert.False(t, ok)
}

func TestServer_removeListener_NotPresent(t *testing.T) {
	s := &Server{}
	s.init()

	ln := &FakeEarlyListener{}
	// Removing a listener that was never added should not panic
	s.removeListener(ln)
}

func TestServer_addListener_NilMap(t *testing.T) {
	s := &Server{}
	// Don't call init — listeners map is nil
	ln := &FakeEarlyListener{}
	s.addListener(ln)

	s.listenerMu.RLock()
	_, ok := s.listeners[ln]
	s.listenerMu.RUnlock()
	assert.True(t, ok)
}

func TestServer_ListenAndServeTLS_ConfiguresDefaults(t *testing.T) {
	called := false
	var gotTLS *tls.Config
	var gotQUIC *quic.Config

	s := &Server{
		Addr: "localhost:0",
		ListenFunc: func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (QUICListener, error) {
			called = true
			gotTLS = tlsConfig
			gotQUIC = quicConfig
			return nil, errors.New("listen failed")
		},
	}

	// Use the example certs if available, otherwise use a temp cert
	certFile := "../examples/cert/localhost.crt"
	keyFile := "../examples/cert/localhost.key"

	err := s.ListenAndServeTLS(certFile, keyFile)
	if err != nil && !called {
		// Cert files may not exist; skip the rest of the test
		t.Skipf("skipping: cert files not available: %v", err)
	}

	assert.Error(t, err) // listen failed
	assert.True(t, called)
	assert.NotNil(t, gotTLS)
	assert.Equal(t, []string{NextProtoH3, NextProtoMOQ}, gotTLS.NextProtos)
	assert.NotNil(t, gotQUIC)
	assert.True(t, gotQUIC.EnableDatagrams)
	assert.True(t, gotQUIC.EnableStreamResetPartialDelivery)
}

func TestNewWebTransportServer(t *testing.T) {
	mux := http.NewServeMux()
	wts := NewWebTransportServer(mux)
	assert.NotNil(t, wts)
}

func TestNewWebTransportServer_NilHandler(t *testing.T) {
	wts := NewWebTransportServer(nil)
	assert.NotNil(t, wts)
}
