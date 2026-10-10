package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"

	"github.com/okdaichi/qmux-go/qmux"
	"github.com/qumo-dev/gomoqt/moqt/internal/qmuxgo"
	"github.com/qumo-dev/gomoqt/transport"
)

// WebSocketHandler upgrades HTTP requests to MOQ sessions over QMux on
// WebSocket: the binding for clients that cannot use WebTransport, such as
// browsers on WebKit. It is the counterpart of WebTransportHandler for an
// HTTP/1.1 server, and a session it serves behaves as a WebTransport one:
// the request URI is the session's path, and SETUP carries no Path.
//
// QMux brings QUIC's streams to the WebSocket, but everything shares one TCP
// connection: a lost segment delays every stream, and there are no
// unreliable datagrams.
//
// The client offers the subprotocol NextProtoQMux, "qmux-02.moq-lite-05":
// the QMux draft and the MOQ version this package speaks. A request that
// does not offer it is refused with 400.
type WebSocketHandler struct {
	Config   *Config
	TrackMux *TrackMux

	// CheckOrigin validates the Origin of an upgrade request. Browsers do
	// not apply CORS to WebSocket, so a server reachable from browsers
	// must set it. If nil, only same-origin requests are accepted.
	CheckOrigin func(r *http.Request) bool

	// QMuxConfig configures the QMux connections. A zero KeepAlivePeriod
	// is 10 seconds; a negative one sends no keep-alive pings.
	QMuxConfig *qmux.Config

	// Handler handles the session after the upgrade. If nil, no request
	// is upgraded.
	Handler Handler

	// FetchHandler handles incoming fetch requests. Optional; when nil,
	// fetch requests are not handled.
	FetchHandler FetchHandler

	// Server, if set, is the Server the sessions of this handler belong
	// to: its Shutdown and Close reach them, it stops the handler from
	// taking new ones, and its ConnContext gives them their context, as it
	// does for the Server's other sessions. A value of the upgrade request
	// hides one ConnContext set under the same key. Without Server the
	// handler stands alone, as a WebTransportHandler does outside one.
	Server *Server

	// UpgradeFunc performs the upgrade in place of the default one. A
	// request it fails must have been answered.
	UpgradeFunc func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error)

	// Logger for events and errors. Optional; if nil, logging is disabled.
	Logger *slog.Logger
}

// upgrader accepts the one application protocol the session layer speaks:
// a subprotocol it agreed to and then did not speak would garble the session.
func (h *WebSocketHandler) upgrader() *qmuxgo.Upgrader {
	return &qmuxgo.Upgrader{
		CheckOrigin: h.CheckOrigin,
		Protocols:   []string{NextProtoMOQ},
		Config:      h.QMuxConfig,
	}
}

// Accepts reports whether r is a WebSocket upgrade that offers the
// subprotocol the handler speaks. It does not check the Origin: call
// CheckOrigin for that. A server that takes WebTransport and WebSocket on
// one route tells them apart with it, and one that decides something of its
// own before the upgrade, such as whether to admit the session, asks first
// whether the upgrade would be refused anyway.
func (h *WebSocketHandler) Accepts(r *http.Request) bool {
	return h.upgrader().Accepts(r)
}

func (h *WebSocketHandler) upgrade(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
	if h.UpgradeFunc != nil {
		return h.UpgradeFunc(w, r)
	}
	conn, err := h.upgrader().Upgrade(w, r)
	var uerr *qmuxgo.UpgradeError
	if errors.As(err, &uerr) && uerr.Status != 0 {
		http.Error(w, uerr.Err.Error(), uerr.Status)
	}
	return conn, err
}

// layeredContext is a context whose values come from itself first, and
// from under for the keys it does not have. A WebSocket session's context
// is the upgrade request's over its Server's ConnContext, as a WebTransport
// session's request context derives from its connection's.
type layeredContext struct {
	context.Context
	under context.Context
}

func (c layeredContext) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.under.Value(key)
}

// ServeHTTP upgrades the request to a session and serves it with Handler,
// returning when the session ends. A request that is not upgraded has been
// answered with an HTTP error.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Handler == nil {
		http.Error(w, "no handler configured", http.StatusServiceUnavailable)
		return
	}
	s := h.Server
	if s != nil {
		s.init()
		if s.shuttingDown() {
			http.Error(w, ErrServerClosed.Error(), http.StatusServiceUnavailable)
			return
		}
	}

	conn, err := h.upgrade(w, r)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Debug("websocket upgrade failed", "error", err)
		}
		return
	}

	// As on WebTransport, the session takes its values from the upgrade
	// request and ends with the transport, which carries the close reason.
	setup := sessionSetup{path: requestPath(r), ctx: r.Context(), lifetime: context.Background()}
	var manager *connManager
	if s != nil {
		// The upgrade took time: the Server may have shut down meanwhile,
		// and would not know of this session.
		manager = s.currentConnManager()
		if manager == nil {
			_ = conn.CloseWithError(transport.ConnErrorCode(NoError), "server shutdown") // not actionable: the session is refused
			return
		}
		connCtx := s.connContext(conn.Context(), conn)
		setup.ctx = layeredContext{Context: r.Context(), under: connCtx}
		setup.lifetime = connCtx
	}
	sess := newSession(conn, h.TrackMux, manager, h.Config, h.FetchHandler, nil, h.Logger, setup, nil)
	// Clean up when the Handler returns, even if it did not close the
	// session itself. Idempotent.
	defer sess.CloseWithError(NoError, "session ended")
	if s != nil && s.shuttingDown() {
		// Shut down between the check above and the session joining the
		// manager: nothing else would close it.
		return
	}

	h.Handler.ServeMOQ(sess)
}

// dialWebSocket opens a WebSocket to rawURL and starts a QMux session on it.
func dialWebSocket(ctx context.Context, rawURL string, header http.Header, tlsConfig *tls.Config, config *qmux.Config) (*http.Response, WebTransportSession, error) {
	return qmuxgo.Dial(ctx, rawURL, header, tlsConfig, []string{NextProtoMOQ}, config)
}
