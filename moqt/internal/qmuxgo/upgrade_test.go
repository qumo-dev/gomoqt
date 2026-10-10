package qmuxgo

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/okdaichi/qmux-go/qmux"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProtocol = "moq-test"

// newUpgradeServer serves u on a test server. Each upgraded session is
// sent to the returned channel, and stays open until the test ends.
func newUpgradeServer(tb testing.TB, u *Upgrader) (string, <-chan transport.WebTransportSession) {
	tb.Helper()
	sessions := make(chan transport.WebTransportSession, 1)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := u.Upgrade(w, r)
		var uerr *UpgradeError
		if errors.As(err, &uerr) && uerr.Status != 0 {
			http.Error(w, uerr.Error(), uerr.Status)
		}
		if err != nil {
			return
		}
		sessions <- sess
		select {
		case <-sess.Context().Done():
		case <-done:
		}
	}))
	tb.Cleanup(func() {
		close(done)
		srv.Close()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http"), sessions
}

func testContext(tb testing.TB) context.Context {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	tb.Cleanup(cancel)
	return ctx
}

// A dialed session and an upgraded one carry streams both ways, and agree
// on the application protocol.
func TestUpgrader_Upgrade(t *testing.T) {
	url, sessions := newUpgradeServer(t, &Upgrader{Protocols: []string{"moq-other", testProtocol}})
	ctx := testContext(t)

	rsp, client, err := Dial(ctx, url+"/room?x=1", nil, nil, []string{testProtocol}, nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseWithError(0, "") }() // not actionable: the test is over
	server := <-sessions

	assert.Equal(t, http.StatusSwitchingProtocols, rsp.StatusCode)
	assert.Equal(t, testProtocol, client.Subprotocol())
	assert.Equal(t, testProtocol, server.Subprotocol())
	assert.Nil(t, client.TLS(), "the test server speaks plain HTTP")
	assert.Nil(t, server.TLS())
	assert.IsType(t, &net.TCPAddr{}, server.RemoteAddr())
	assert.IsType(t, &net.TCPAddr{}, server.LocalAddr())
	assert.Equal(t, server.LocalAddr().String(), client.RemoteAddr().String(), "the client reports the socket it dialed")
	assert.Equal(t, server.RemoteAddr().String(), client.LocalAddr().String())

	// Client to server, on a unidirectional stream.
	send, err := client.OpenUniStreamSync(ctx)
	require.NoError(t, err)
	_, err = send.Write([]byte("up"))
	require.NoError(t, err)
	require.NoError(t, send.Close())
	recv, err := server.AcceptUniStream(ctx)
	require.NoError(t, err)
	got, err := io.ReadAll(recv)
	require.NoError(t, err)
	assert.Equal(t, "up", string(got))

	// Server to client and back, on a bidirectional stream.
	down, err := server.OpenStreamSync(ctx)
	require.NoError(t, err)
	_, err = down.Write([]byte("down"))
	require.NoError(t, err)
	require.NoError(t, down.Close())
	echo, err := client.AcceptStream(ctx)
	require.NoError(t, err)
	got, err = io.ReadAll(echo)
	require.NoError(t, err)
	assert.Equal(t, "down", string(got))

	// The non-blocking opens work once the handshake is done.
	_, err = client.OpenStream() //nolint:staticcheck // deprecated in the interface, and still the adapter's to implement
	require.NoError(t, err)
	_, err = client.OpenUniStream() //nolint:staticcheck // deprecated in the interface, and still the adapter's to implement
	require.NoError(t, err)

	stats, ok := server.(interface {
		ConnectionStats() transport.ConnectionStats
	})
	require.True(t, ok, "the session reports connection statistics")
	assert.Positive(t, stats.ConnectionStats().BytesReceived)
}

// A close carries its code and reason to the peer.
func TestSession_CloseWithError(t *testing.T) {
	url, sessions := newUpgradeServer(t, &Upgrader{Protocols: []string{testProtocol}})
	ctx := testContext(t)
	_, client, err := Dial(ctx, url, nil, nil, []string{testProtocol}, nil)
	require.NoError(t, err)
	server := <-sessions

	require.NoError(t, client.CloseWithError(7, "bye"))

	select {
	case <-server.Context().Done():
	case <-ctx.Done():
		require.FailNow(t, "the server session outlived the client's close")
	}
	var appErr *transport.ApplicationError
	require.ErrorAs(t, context.Cause(server.Context()), &appErr)
	assert.True(t, appErr.Remote)
	assert.Equal(t, transport.ApplicationErrorCode(7), appErr.ErrorCode)
	assert.Equal(t, "bye", appErr.ErrorMessage)

	_, err = server.AcceptStream(ctx)
	assert.ErrorAs(t, err, &appErr)
	_, err = server.AcceptUniStream(ctx)
	assert.ErrorAs(t, err, &appErr)
	_, err = server.OpenStreamSync(ctx)
	assert.ErrorAs(t, err, &appErr)
	_, err = server.OpenUniStreamSync(ctx)
	assert.ErrorAs(t, err, &appErr)
	_, err = server.OpenStream() //nolint:staticcheck // deprecated in the interface, and still the adapter's to implement
	assert.ErrorAs(t, err, &appErr)
	_, err = server.OpenUniStream() //nolint:staticcheck // deprecated in the interface, and still the adapter's to implement
	assert.ErrorAs(t, err, &appErr)
}

func TestUpgrader_Upgrade_Refused(t *testing.T) {
	tests := map[string]struct {
		upgrader *Upgrader
		// subprotocols offered; nil sends a plain GET instead.
		subprotocols []string
		origin       string
		status       int
	}{
		"not a WebSocket upgrade": {
			upgrader: &Upgrader{Protocols: []string{testProtocol}},
			status:   http.StatusUpgradeRequired,
		},
		"no supported subprotocol": {
			upgrader:     &Upgrader{Protocols: []string{testProtocol}},
			subprotocols: []string{"qmux-02.other"},
			status:       http.StatusBadRequest,
		},
		"origin refused by CheckOrigin": {
			upgrader:     &Upgrader{Protocols: []string{testProtocol}, CheckOrigin: func(*http.Request) bool { return false }},
			subprotocols: []string{Version + "." + testProtocol},
			status:       http.StatusForbidden,
		},
		"cross-origin without CheckOrigin": {
			upgrader:     &Upgrader{Protocols: []string{testProtocol}},
			subprotocols: []string{Version + "." + testProtocol},
			origin:       "https://evil.example",
			status:       http.StatusForbidden,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			url, _ := newUpgradeServer(t, tt.upgrader)
			ctx := testContext(t)

			if tt.subprotocols == nil {
				rsp, err := http.Get("http" + strings.TrimPrefix(url, "ws"))
				require.NoError(t, err)
				defer func() { _ = rsp.Body.Close() }() // not actionable: the test is over
				assert.Equal(t, tt.status, rsp.StatusCode)
				return
			}
			header := http.Header{}
			if tt.origin != "" {
				header.Set("Origin", tt.origin)
			}
			_, rsp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: tt.subprotocols, HTTPHeader: header})
			require.Error(t, err)
			require.NotNil(t, rsp)
			assert.Equal(t, tt.status, rsp.StatusCode)
		})
	}
}

// A cross-origin request passes once CheckOrigin allows it.
func TestUpgrader_Upgrade_CheckOriginAllows(t *testing.T) {
	url, sessions := newUpgradeServer(t, &Upgrader{
		Protocols:   []string{testProtocol},
		CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://app.example" },
	})
	ctx := testContext(t)

	_, client, err := Dial(ctx, url, http.Header{"Origin": {"https://app.example"}}, nil, []string{testProtocol}, nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseWithError(0, "") }() // not actionable: the test is over

	assert.NotNil(t, <-sessions)
}

// A peer that upgrades and then does not speak QMux fails the upgrade.
func TestUpgrader_Upgrade_HandshakeFails(t *testing.T) {
	upgraded := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := &Upgrader{Protocols: []string{testProtocol}}
		_, err := u.Upgrade(w, r)
		upgraded <- err
	}))
	defer srv.Close()
	ctx := testContext(t)

	ws, _, err := websocket.Dial(ctx, srv.URL, &websocket.DialOptions{Subprotocols: []string{Version + "." + testProtocol}})
	require.NoError(t, err)
	defer func() { _ = ws.CloseNow() }() // not actionable: the test is over
	// A text message is not a QMux record.
	require.NoError(t, ws.Write(ctx, websocket.MessageText, []byte("hello")))

	var uerr *UpgradeError
	select {
	case err := <-upgraded:
		require.ErrorAs(t, err, &uerr)
		assert.Zero(t, uerr.Status, "the response was already written")
	case <-ctx.Done():
		require.FailNow(t, "the upgrade did not fail")
	}
}

func TestDial_Refused(t *testing.T) {
	tests := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"the server is not a WebSocket server": {
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusNotFound) },
			want:    "404",
		},
		"the server selects no subprotocol": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				ws, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				_, _, _ = ws.Read(r.Context()) // not actionable: waits for the client to hang up
			},
			want: "subprotocol",
		},
		"the server does not speak QMux": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{Version + "." + testProtocol}})
				if err != nil {
					return
				}
				_ = ws.Write(r.Context(), websocket.MessageText, []byte("hello")) // not actionable: the client fails on it
				_, _, _ = ws.Read(r.Context())                                    // not actionable: waits for the client to hang up
			},
			want: "text message",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			_, sess, err := Dial(testContext(t), "ws"+strings.TrimPrefix(srv.URL, "http"), nil, nil, []string{testProtocol}, nil)

			require.Error(t, err)
			assert.Nil(t, sess)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestWithDefaults(t *testing.T) {
	tests := map[string]struct {
		config   *qmux.Config
		expected *qmux.Config
	}{
		"nil": {
			config:   nil,
			expected: &qmux.Config{KeepAlivePeriod: DefaultKeepAlivePeriod},
		},
		"another field set keeps the keep-alive": {
			config:   &qmux.Config{MaxIncomingStreams: 7},
			expected: &qmux.Config{MaxIncomingStreams: 7, KeepAlivePeriod: DefaultKeepAlivePeriod},
		},
		"a keep-alive period of the caller's": {
			config:   &qmux.Config{KeepAlivePeriod: time.Second},
			expected: &qmux.Config{KeepAlivePeriod: time.Second},
		},
		"a negative period sends no pings": {
			config:   &qmux.Config{KeepAlivePeriod: -1},
			expected: &qmux.Config{KeepAlivePeriod: -1},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := withDefaults(tt.config)

			assert.Equal(t, tt.expected, got)
			assert.NotSame(t, tt.config, got, "the caller's configuration is not modified")
		})
	}
}

func TestUpgrader_Accepts(t *testing.T) {
	u := &Upgrader{Protocols: []string{testProtocol}}
	upgrade := func(subprotocol string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		if subprotocol != "" {
			r.Header.Set("Sec-WebSocket-Protocol", subprotocol)
		}
		return r
	}
	tests := map[string]struct {
		request  *http.Request
		expected bool
	}{
		"an upgrade offering the protocol": {request: upgrade(Version + "." + testProtocol), expected: true},
		"an upgrade offering another":      {request: upgrade(Version + ".other")},
		"an upgrade offering none":         {request: upgrade("")},
		"not an upgrade":                   {request: httptest.NewRequest(http.MethodGet, "/", nil)},
		"a refused Origin is not its matter": {request: func() *http.Request {
			r := upgrade(Version + "." + testProtocol)
			r.Header.Set("Origin", "https://evil.example")
			return r
		}(), expected: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, u.Accepts(tt.request))
		})
	}
}

// A write deadline drops a connection whose peer has stopped reading, so
// that closing it does not wait for the peer.
func TestMessageConn_SetWriteDeadline(t *testing.T) {
	url, sessions := newUpgradeServer(t, &Upgrader{Protocols: []string{testProtocol}})
	ctx := testContext(t)
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{Version + "." + testProtocol}})
	require.NoError(t, err)
	defer func() { _ = ws.CloseNow() }() // not actionable: the test is over
	mc := newMessageConn(ws, &qmux.Config{}, addr("local"), addr("remote"))
	// The server waits for transport parameters that never come.
	_ = sessions

	require.NoError(t, mc.SetWriteDeadline(time.Now().Add(20*time.Millisecond)))

	select {
	case <-mc.ctx.Done():
	case <-ctx.Done():
		require.FailNow(t, "the deadline did not drop the connection")
	}
	assert.Error(t, mc.WriteMessage([]byte("late")))

	// A cleared deadline does nothing, and closing twice is fine.
	require.NoError(t, mc.SetWriteDeadline(time.Time{}))
	require.NoError(t, mc.Close())
	require.NoError(t, mc.Close())
}

func TestUpgradeError(t *testing.T) {
	cause := errors.New("origin not allowed")
	err := &UpgradeError{Status: http.StatusForbidden, Err: cause}

	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "qmuxgo: upgrade: origin not allowed", err.Error())
}
