package moqt

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/okdaichi/gomoqt/moqt/internal/quicgo"
	"github.com/okdaichi/gomoqt/moqt/internal/webtransportgo"
	"github.com/quic-go/quic-go"
)

// Dialer is a MOQ client that can establish sessions with MOQ servers.
// It supports both WebTransport and native QUIC connections.
//
// A Dialer can connect to multiple servers and maintain multiple active sessions.
// When the caller closes a session or shuts down the client lifecycle, active
// sessions are terminated gracefully.
type Dialer struct {
	// TLS configuration for both WebTransport and QUIC connections.
	TLSConfig *tls.Config

	// QUIC configuration for raw QUIC connections.
	QUICConfig *quic.Config

	// Config contains additional configuration options for the Dialer.
	Config *Config

	// DialQUICFunc performs the QUIC handshake and establishes a connection.
	// If nil, the default QUIC dialer is used.
	DialQUICFunc func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error)

	// DialWebTransportFunc performs the WebTransport handshake and establishes a connection.
	// If nil, the default dialer is used.
	DialWebTransportFunc func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error)

	// FetchHandler handles incoming fetch requests on WebTransport sessions.
	// If nil, fetch requests on WebTransport sessions are not handled.
	FetchHandler FetchHandler

	// OnGoaway is called when a GOAWAY message is received from the server.
	// The newSessionURI parameter contains the redirect URI, which may be empty.
	OnGoaway func(newSessionURI string)

	// Logger is used for logging connection and session events. If nil, logging is disabled.
	Logger *slog.Logger
}

// Dial establishes a new session to the specified URL using either WebTransport (https scheme) or QUIC (moqt scheme).
// The provided TrackMux is used to route incoming service tracks if non-nil.
// Dial returns the newly created Session or an error.
func (d *Dialer) Dial(ctx context.Context, urlStr string, mux *TrackMux) (*Session, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Dial based on the scheme
	switch parsedURL.Scheme {
	case "https":
		return d.DialWebTransport(ctx, parsedURL.Host, parsedURL.Path, mux)
	case "moqt":
		return d.DialQUIC(ctx, parsedURL.Host, parsedURL.Path, mux)
	default:
		return nil, ErrInvalidScheme
	}
}

// DialWebTransport establishes a new session over WebTransport (HTTP/3).
// It performs the WebTransport handshake and initializes a MOQ session.
// `host` should be host:port and `path` is the path used for session setup.
func (d *Dialer) DialWebTransport(ctx context.Context, host, path string, mux *TrackMux) (*Session, error) {
	var baseLogger *slog.Logger
	if d.Logger != nil {
		baseLogger = d.Logger
	} else {
		baseLogger = slog.New(slog.DiscardHandler)
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, d.Config.setupTimeout())
	defer cancelDial()

	var dialer func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error)
	if d.DialWebTransportFunc != nil {
		dialer = d.DialWebTransportFunc
	} else {
		dialer = webtransportgo.Dial
	}
	target := host
	if !strings.Contains(target, "://") {
		if path == "" {
			path = "/"
		}
		target = "https://" + host + path
	}

	_, conn, err := dialer(dialCtx, target, nil, d.TLSConfig)
	if err != nil {
		return nil, err
	}

	connLogger := baseLogger.With(
		"transport", "webtransport",
		"local_address", conn.LocalAddr(),
		"remote_address", conn.RemoteAddr(),
	)
	connLogger.Info("connection established")

	return newSession(conn, mux, nil, d.FetchHandler, d.OnGoaway, d.Logger), nil
}

// DialQUIC establishes a new session over native QUIC by dialing the provided
// address and negotiating the transport protocol. This uses the QUIC dial
// function configured on the Dialer (DialQUICFunc) if present.
func (d *Dialer) DialQUIC(ctx context.Context, addr, path string, mux *TrackMux) (*Session, error) {
	dialTimeout := d.Config.setupTimeout()
	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	defer cancelDial()

	tlsConfig := d.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if len(tlsConfig.NextProtos) == 0 {
		tlsConfig.NextProtos = []string{NextProtoMOQ}
	}

	var dialFunc func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error)
	if d.DialQUICFunc != nil {
		dialFunc = d.DialQUICFunc
	} else {
		dialFunc = quicgo.DialAddrEarly
	}
	conn, err := dialFunc(dialCtx, addr, tlsConfig, d.QUICConfig)
	if err != nil {
		return nil, err
	}

	return newSession(conn, mux, nil, d.FetchHandler, d.OnGoaway, d.Logger), nil
}
