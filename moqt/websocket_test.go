package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsURL returns the WebSocket URL of a path on an httptest server.
func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// A publisher on the server and a subscriber over WebSocket, with far more
// groups than the transport's stream limit of 100: one stream per group has
// to keep working past it.
func TestWebSocketHandler_ServeHTTP(t *testing.T) {
	const groups = 300
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mux := NewTrackMux(0)
	mux.PublishFunc(ctx, "/live", func(tw *TrackWriter) {
		frame := NewFrame(16)
		for i := range groups {
			gw, err := tw.OpenGroup(ctx)
			if err != nil {
				return
			}
			frame.Reset()
			_, _ = fmt.Fprintf(frame, "frame-%d", i) // not actionable: a Frame's Write does not fail
			if err := gw.WriteFrame(frame); err != nil {
				return
			}
			if err := gw.Close(); err != nil {
				return
			}
		}
		<-tw.Context().Done()
	})

	served := make(chan *Session, 1)
	srv := httptest.NewServer(&WebSocketHandler{
		TrackMux: mux,
		Handler: HandleFunc(func(sess *Session) {
			served <- sess
			<-sess.Context().Done()
		}),
	})
	defer srv.Close()

	sess, err := (&Dialer{}).Dial(ctx, wsURL(srv, "/room/a?token=x"), nil)
	require.NoError(t, err)
	defer func() { _ = sess.CloseWithError(NoError, "done") }() // not actionable: the test is over

	// The path travels in the request URI, as on WebTransport.
	assert.Equal(t, "/room/a?token=x", sess.RequestURI())
	select {
	case serverSess := <-served:
		assert.Equal(t, "/room/a?token=x", serverSess.RequestURI())
		assert.Nil(t, serverSess.ConnectionState().TLS, "the test server speaks plain HTTP")
	case <-ctx.Done():
		require.FailNow(t, "the handler did not get the session")
	}

	tr, err := sess.Subscribe(ctx, "/live", "video", nil)
	require.NoError(t, err)
	defer tr.Close()

	frame := NewFrame(16)
	for i := range groups {
		gr, err := tr.AcceptGroup(ctx)
		require.NoError(t, err, "group %d", i)
		require.NoError(t, gr.ReadFrame(frame), "group %d", i)
		// Group sequences start at 1.
		assert.Equal(t, fmt.Sprintf("frame-%d", gr.GroupSequence()-1), string(frame.Body()))
	}
}

func TestWebSocketHandler_ServeHTTP_Refused(t *testing.T) {
	serve := HandleFunc(func(sess *Session) { <-sess.Context().Done() })
	closed := &Server{}
	require.NoError(t, closed.Close())

	tests := map[string]struct {
		handler *WebSocketHandler
		// subprotocols offered; nil sends a plain GET instead.
		subprotocols []string
		origin       string
		status       int
	}{
		"not a WebSocket upgrade": {
			handler: &WebSocketHandler{Handler: serve},
			status:  http.StatusUpgradeRequired,
		},
		"no subprotocol": {
			handler:      &WebSocketHandler{Handler: serve},
			subprotocols: []string{},
			status:       http.StatusBadRequest,
		},
		"another application protocol": {
			handler:      &WebSocketHandler{Handler: serve},
			subprotocols: []string{"qmux-02.moq-lite-99"},
			status:       http.StatusBadRequest,
		},
		"another QMux draft": {
			handler:      &WebSocketHandler{Handler: serve},
			subprotocols: []string{"qmux-01." + NextProtoMOQ, "webtransport"},
			status:       http.StatusBadRequest,
		},
		"origin refused by CheckOrigin": {
			handler:      &WebSocketHandler{Handler: serve, CheckOrigin: func(*http.Request) bool { return false }},
			subprotocols: []string{NextProtoQMux},
			origin:       "https://evil.example",
			status:       http.StatusForbidden,
		},
		"cross-origin without CheckOrigin": {
			handler:      &WebSocketHandler{Handler: serve},
			subprotocols: []string{NextProtoQMux},
			origin:       "https://evil.example",
			status:       http.StatusForbidden,
		},
		"no handler": {
			handler:      &WebSocketHandler{},
			subprotocols: []string{NextProtoQMux},
			status:       http.StatusServiceUnavailable,
		},
		"server closed": {
			handler:      &WebSocketHandler{Handler: serve, Server: closed},
			subprotocols: []string{NextProtoQMux},
			status:       http.StatusServiceUnavailable,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if tt.subprotocols == nil {
				rsp, err := http.Get(srv.URL)
				require.NoError(t, err)
				defer func() { _ = rsp.Body.Close() }() // not actionable: the test is over
				assert.Equal(t, tt.status, rsp.StatusCode)
				return
			}
			header := http.Header{}
			if tt.origin != "" {
				header.Set("Origin", tt.origin)
			}
			_, rsp, err := websocket.Dial(ctx, srv.URL, &websocket.DialOptions{
				Subprotocols: tt.subprotocols,
				HTTPHeader:   header,
			})
			require.Error(t, err)
			require.NotNil(t, rsp)
			assert.Equal(t, tt.status, rsp.StatusCode)
		})
	}
}

// With Server set, closing the Server ends the handler's sessions.
func TestWebSocketHandler_ServeHTTP_ServerClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := &Server{}
	srv := httptest.NewServer(&WebSocketHandler{
		Server:  server,
		Handler: HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	})
	defer srv.Close()

	sess, err := (&Dialer{}).Dial(ctx, wsURL(srv, "/"), nil)
	require.NoError(t, err)
	// The server tracks the session once its handler runs.
	require.Eventually(t, func() bool { return server.currentConnManager().countSessions() == 1 },
		5*time.Second, 5*time.Millisecond)

	closeServer(t, server)

	select {
	case <-sess.Context().Done():
		var serr *SessionError
		require.ErrorAs(t, Cause(sess.Context()), &serr)
		assert.True(t, serr.Remote)
	case <-ctx.Done():
		require.FailNow(t, "the session outlived its Server")
	}
}

// With Server set, shutting the Server down sends the handler's sessions a
// GOAWAY with the next session URI, as it does its other sessions.
func TestWebSocketHandler_ServeHTTP_ServerShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := &Server{NextSessionURI: "https://next.example/session"}
	srv := httptest.NewServer(&WebSocketHandler{
		Server:  server,
		Handler: HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	})
	defer srv.Close()

	goaway := make(chan string, 1)
	sess, err := (&Dialer{OnGoaway: func(uri string) { goaway <- uri }}).Dial(ctx, wsURL(srv, "/"), nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return server.currentConnManager().countSessions() == 1 },
		5*time.Second, 5*time.Millisecond)

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(ctx) }()

	select {
	case uri := <-goaway:
		assert.Equal(t, "https://next.example/session", uri)
	case <-ctx.Done():
		require.FailNow(t, "the session got no GOAWAY")
	}
	// The client leaves, which lets the shutdown finish.
	require.NoError(t, sess.CloseWithError(NoError, "going away"))
	select {
	case err := <-shutdownDone:
		assert.NoError(t, err)
	case <-ctx.Done():
		require.FailNow(t, "Shutdown did not return after the session left")
	}
}

func TestWebSocketHandler_ServeHTTP_UpgradeFunc(t *testing.T) {
	conn := &FakeWebTransportSession{}
	conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
	conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}

	var got *Session
	handler := &WebSocketHandler{
		UpgradeFunc: func(http.ResponseWriter, *http.Request) (WebTransportSession, error) {
			return conn, nil
		},
		Handler: HandleFunc(func(sess *Session) { got = sess }),
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/custom?x=1", nil))

	require.NotNil(t, got)
	assert.Equal(t, "/custom?x=1", got.RequestURI())
}

func TestDialer_Dial_WSSRoutesToDialWebSocket(t *testing.T) {
	tests := map[string]struct {
		url string
	}{
		"wss": {url: "wss://example.com:443/session?x=1"},
		"ws":  {url: "ws://example.com:80/session?x=1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			called := false
			dialer := &Dialer{
				Config: &Config{SetupTimeout: 50 * time.Millisecond},
				DialWebSocketFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
					called = true
					_, ok := ctx.Deadline()
					assert.True(t, ok, "the dial is bounded by the setup timeout")
					assert.Equal(t, tt.url, addr)

					conn := &FakeWebTransportSession{}
					conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
					conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
					conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
					conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
					return &http.Response{StatusCode: http.StatusSwitchingProtocols}, conn, nil
				},
			}

			sess, err := dialer.Dial(context.Background(), tt.url, nil)
			require.NoError(t, err)
			defer func() { _ = sess.CloseWithError(NoError, "") }() // not actionable: the test is over

			assert.True(t, called)
			assert.Equal(t, "/session?x=1", sess.RequestURI())
			assert.False(t, sess.sendPath, "the path travels in the request URI, not in SETUP")
		})
	}
}

func TestDialer_Dial_WebSocketError(t *testing.T) {
	want := errors.New("refused")
	dialer := &Dialer{
		DialWebSocketFunc: func(context.Context, string, http.Header, *tls.Config) (*http.Response, WebTransportSession, error) {
			return nil, nil, want
		},
	}

	sess, err := dialer.Dial(context.Background(), "wss://example.com/", nil)
	assert.ErrorIs(t, err, want)
	assert.Nil(t, sess)
}

// A server that selects no subprotocol, or one that was not offered, has
// not agreed to speak MOQ over QMux.
func TestDialer_Dial_WebSocketWithoutSubprotocol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_, _, _ = ws.Read(r.Context()) // not actionable: waits for the client to hang up
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := (&Dialer{}).Dial(ctx, wsURL(srv, "/"), nil)
	require.Error(t, err)
	assert.Nil(t, sess)
	assert.NotErrorIs(t, err, io.EOF)
	assert.Contains(t, err.Error(), "subprotocol")
}

// With Server set, a session has the values of the Server's ConnContext,
// under those of its upgrade request.
func TestWebSocketHandler_ServeHTTP_ConnContext(t *testing.T) {
	type key string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := &Server{
		ConnContext: func(ctx context.Context, _ StreamConn) context.Context {
			ctx = context.WithValue(ctx, key("conn"), "from ConnContext")
			return context.WithValue(ctx, key("both"), "from ConnContext")
		},
	}
	defer closeServer(t, server)
	served := make(chan *Session, 1)
	handler := &WebSocketHandler{
		Server: server,
		Handler: HandleFunc(func(sess *Session) {
			served <- sess
			<-sess.Context().Done()
		}),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key("both"), "from the request")))
	}))
	defer srv.Close()

	client, err := (&Dialer{}).Dial(ctx, wsURL(srv, "/"), nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseWithError(NoError, "done") }() // not actionable: the test is over

	select {
	case sess := <-served:
		assert.Equal(t, "from ConnContext", sess.Context().Value(key("conn")))
		assert.Equal(t, "from the request", sess.Context().Value(key("both")), "the request's value hides ConnContext's")
		assert.Nil(t, sess.Context().Value(key("neither")))
	case <-ctx.Done():
		require.FailNow(t, "the handler did not get the session")
	}
}

func TestWebSocketHandler_Accepts(t *testing.T) {
	upgrade := func(subprotocol string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Protocol", subprotocol)
		return r
	}
	tests := map[string]struct {
		handler  *WebSocketHandler
		request  *http.Request
		expected bool
	}{
		"the default protocol": {
			handler: &WebSocketHandler{}, request: upgrade(NextProtoQMux), expected: true,
		},
		"another protocol": {
			handler: &WebSocketHandler{}, request: upgrade("qmux-02.moq-lite-99"),
		},
		"not a WebSocket upgrade": {
			handler: &WebSocketHandler{}, request: httptest.NewRequest(http.MethodGet, "/", nil),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.handler.Accepts(tt.request))
		})
	}
}

// A handler's sessions are configured by its QUICConfig, or else by its
// Server's: limits are set once for every transport.
func TestWebSocketHandler_upgrader_Config(t *testing.T) {
	own := &quic.Config{MaxIncomingStreams: 1}
	servers := &quic.Config{MaxIncomingStreams: 2}
	tests := map[string]struct {
		handler  *WebSocketHandler
		expected *quic.Config
	}{
		"its own":                   {handler: &WebSocketHandler{QUICConfig: own}, expected: own},
		"its own over the Server's": {handler: &WebSocketHandler{QUICConfig: own, Server: &Server{QUICConfig: servers}}, expected: own},
		"the Server's":              {handler: &WebSocketHandler{Server: &Server{QUICConfig: servers}}, expected: servers},
		"a Server without one":      {handler: &WebSocketHandler{Server: &Server{}}, expected: nil},
		"neither":                   {handler: &WebSocketHandler{}, expected: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.handler.upgrader().Config

			assert.Same(t, tt.expected, got)
		})
	}
}
