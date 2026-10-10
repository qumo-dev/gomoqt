package moqt

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/qmux"
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

	// QUICConfig configures the sessions with the settings QMux shares
	// with QUIC: the stream limits, the receive windows, the keep-alive
	// period, the idle and handshake timeouts, and datagrams. The rest of
	// it has no meaning over WebSocket. If nil, Server's QUICConfig is
	// used. A zero KeepAlivePeriod is 10 seconds here, where QUIC would
	// send none: an idle WebSocket is closed by the proxies on its way. A
	// negative one sends none.
	QUICConfig *quic.Config

	// Handler handles the session after the upgrade. If nil, no request
	// is upgraded.
	Handler Handler

	// FetchHandler handles incoming fetch requests. Optional; when nil,
	// fetch requests are not handled.
	FetchHandler FetchHandler

	// Server, if set, is the Server the sessions of this handler belong
	// to: its Shutdown and Close reach them, it stops the handler from
	// taking new ones, its QUICConfig configures them unless QUICConfig
	// does, and its ConnContext derives their context from the upgrade
	// request's, as it derives its QUIC connections'. Without Server the
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
func (h *WebSocketHandler) upgrader() *qmux.Upgrader {
	config := h.QUICConfig
	if config == nil && h.Server != nil {
		config = h.Server.QUICConfig
	}
	return &qmux.Upgrader{
		CheckOrigin: h.CheckOrigin,
		Protocols:   []string{NextProtoMOQ},
		Config:      config,
	}
}

// Accepts reports whether ServeHTTP would upgrade r, as far as can be told
// before trying: r is a WebSocket upgrade that offers the subprotocol the
// handler speaks, and the handler has a Handler and a Server, if any, that
// is not shutting down. It does not check the Origin: call CheckOrigin for
// that. A server that takes WebTransport and WebSocket on one route tells
// them apart with it, and one that decides something of its own before the
// upgrade, such as whether to admit the session, asks first whether the
// upgrade would be refused anyway.
func (h *WebSocketHandler) Accepts(r *http.Request) bool {
	if h.Handler == nil || (h.Server != nil && h.Server.shuttingDown()) {
		return false
	}
	return h.upgrader().Accepts(r)
}

func (h *WebSocketHandler) upgrade(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
	if h.UpgradeFunc != nil {
		return h.UpgradeFunc(w, r)
	}
	conn, err := h.upgrader().Upgrade(w, r)
	var uerr *qmux.UpgradeError
	if errors.As(err, &uerr) && uerr.Status != 0 {
		http.Error(w, uerr.Err.Error(), uerr.Status)
	}
	return conn, err
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
		// The Server's ConnContext derives the session's context from the
		// upgrade request's, as it derives a QUIC connection's from the
		// connection's. Cancelling what it returns ends the session.
		ctx := s.connContext(r.Context(), conn)
		setup.ctx, setup.lifetime = ctx, ctx
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
