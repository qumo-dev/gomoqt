package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"

	"github.com/okdaichi/qmux-go/qmux"
	"github.com/qumo-dev/gomoqt/moqt/internal/qmuxgo"
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
// The client offers the subprotocol "qmux-02.<protocol>" for each
// application protocol it speaks, as NextProtoQMux for this package's. A
// request that offers none the handler supports is refused with 400.
type WebSocketHandler struct {
	Config   *Config
	TrackMux *TrackMux

	// CheckOrigin validates the Origin of an upgrade request. Browsers do
	// not apply CORS to WebSocket, so a server reachable from browsers
	// must set it. If nil, only same-origin requests are accepted.
	CheckOrigin func(r *http.Request) bool

	// ApplicationProtocols lists the application protocols accepted. If
	// empty, NextProtoMOQ is.
	ApplicationProtocols []string

	// QMuxConfig configures the QMux connections. If nil, the defaults
	// apply, with a keep-alive ping every 10 seconds.
	QMuxConfig *qmux.Config

	// Handler handles the session after the upgrade. If nil, no request
	// is upgraded.
	Handler Handler

	// FetchHandler handles incoming fetch requests. Optional; when nil,
	// fetch requests are not handled.
	FetchHandler FetchHandler

	// Server, if set, is the Server whose Shutdown and Close reach the
	// sessions of this handler, and which stops the handler from taking
	// new ones. Without it the handler stands alone, as a
	// WebTransportHandler does outside a Server.
	Server *Server

	// UpgradeFunc performs the upgrade in place of the default one. A
	// request it fails must have been answered.
	UpgradeFunc func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error)

	// Logger for events and errors. Optional; if nil, logging is disabled.
	Logger *slog.Logger
}

func (h *WebSocketHandler) upgrade(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
	if h.UpgradeFunc != nil {
		return h.UpgradeFunc(w, r)
	}
	protocols := h.ApplicationProtocols
	if len(protocols) == 0 {
		protocols = []string{NextProtoMOQ}
	}
	upgrader := qmuxgo.Upgrader{
		CheckOrigin: h.CheckOrigin,
		Protocols:   protocols,
		Config:      h.QMuxConfig,
	}
	conn, err := upgrader.Upgrade(w, r)
	var uerr *qmuxgo.UpgradeError
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
	var manager *connManager
	if s := h.Server; s != nil {
		s.init()
		if s.shuttingDown() {
			http.Error(w, ErrServerClosed.Error(), http.StatusServiceUnavailable)
			return
		}
		manager = s.connManager
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
	sess := newSession(conn, h.TrackMux, manager, h.Config, h.FetchHandler, nil, h.Logger,
		sessionSetup{path: requestPath(r), ctx: r.Context(), lifetime: context.Background()}, nil)
	// Clean up when the Handler returns, even if it did not close the
	// session itself. Idempotent.
	defer sess.CloseWithError(NoError, "session ended")

	h.Handler.ServeMOQ(sess)
}

// dialWebSocket opens a WebSocket to rawURL and starts a QMux session on it.
func dialWebSocket(ctx context.Context, rawURL string, header http.Header, tlsConfig *tls.Config, config *qmux.Config) (*http.Response, WebTransportSession, error) {
	return qmuxgo.Dial(ctx, rawURL, header, tlsConfig, []string{NextProtoMOQ}, config)
}
