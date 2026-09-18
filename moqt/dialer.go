package moqt

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/quicgo"
	"github.com/qumo-dev/gomoqt/moqt/internal/webtransportgo"
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

// Dial establishes a new session with the server named by rawURL, and is the
// only entry point for doing so: the URL's scheme selects the transport
// binding, and its path selects the server-side endpoint.
//
// Scheme "https" dials WebTransport and "moqt" dials native QUIC; any other
// scheme returns ErrInvalidScheme. The path is reported by Session.RequestPath and, on
// native QUIC, conveyed to the server as the SETUP Path parameter. A URL with no
// path is dialed as "/".
//
// A query is carried only by "https", where it belongs to the HTTP request URI
// the server receives. On native QUIC the path travels as the moq-lite SETUP
// Path parameter, whose value is defined as a URI path (draft-lcurley-moq-lite-05),
// with no place for a query — so a "moqt" URL carrying one returns
// ErrQueryNotSupported rather than connecting to an endpoint the caller did not
// ask for. Note this is narrower than draft-ietf-moq-transport, whose "moqt"
// URI grammar admits a query and whose PATH parameter carries it appended to
// the path; gomoqt speaks moq-lite, so that form is not representable here.
//
// Userinfo and a fragment are client-side only and are never sent on either
// binding.
//
// The provided TrackMux is used to route incoming service tracks if non-nil.
func (d *Dialer) Dial(ctx context.Context, rawURL string, mux *TrackMux) (*Session, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	// Normalize once, here, so neither binding has to: both the WebTransport
	// request URI and the native-QUIC Path parameter must be rooted at "/" (the
	// server rejects a SETUP Path that is not).
	target := *parsed
	target.Fragment, target.RawFragment = "", ""
	// Userinfo reaches neither binding's wire format, and the pre-collapse code
	// dropped it when it rebuilt the target from host and path. Clear it so it
	// cannot reach a caller-supplied DialWebTransportFunc, or a log line.
	target.User = nil
	// A bare "?" carries no query. Treat it as absent on both bindings, rather
	// than dialing a trailing "?" on one and rejecting the URL on the other.
	if target.RawQuery == "" {
		target.ForceQuery = false
	}
	if target.Path == "" {
		target.Path = "/"
	}

	switch target.Scheme {
	case "https":
		return d.dialWebTransport(ctx, &target, mux)
	case "moqt":
		if target.RawQuery != "" {
			// Report the endpoint without the query: it is present by
			// construction here, commonly carries a token, and errors are
			// routinely logged.
			return nil, fmt.Errorf("%w: %s://%s%s", ErrQueryNotSupported,
				target.Scheme, target.Host, target.Path)
		}
		return d.dialQUIC(ctx, target.Host, target.Path, mux)
	default:
		return nil, ErrInvalidScheme
	}
}

// dialWebTransport establishes a new session over WebTransport (HTTP/3). It
// performs the WebTransport handshake and initializes a MOQ session.
//
// target is the request URI to dial, already normalized by Dial: an absolute
// https URL whose path is rooted at "/". It is dialed verbatim, so whatever it
// carries — path and query alike — reaches the server's http.Handler.
func (d *Dialer) dialWebTransport(ctx context.Context, target *url.URL, mux *TrackMux) (*Session, error) {
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
		dialer = func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			return webtransportgo.Dial(ctx, addr, header, tlsConfig, []string{NextProtoMOQ})
		}
	}
	_, conn, err := dialer(dialCtx, target.String(), nil, d.TLSConfig)
	if err != nil {
		return nil, err
	}

	connLogger := baseLogger.With(
		"transport", "webtransport",
		"local_address", conn.LocalAddr(),
		"remote_address", conn.RemoteAddr(),
	)
	connLogger.Info("connection established")

	// WebTransport binds the request path in the HTTP handshake, so the peer
	// learns it from the request URI and this endpoint must not send a SETUP
	// Path parameter.
	return newSession(conn, mux, nil, d.Config, d.FetchHandler, d.OnGoaway, d.Logger,
		sessionSetup{path: target.Path}, nil), nil
}

// dialQUIC establishes a new session over native QUIC by dialing the provided
// address and negotiating the transport protocol. This uses the QUIC dial
// function configured on the Dialer (DialQUICFunc) if present.
//
// path is the request path conveyed to the server via the SETUP Path parameter,
// since the native QUIC binding has no request URI of its own. Dial is the only
// caller and has already normalized it to a rooted, non-empty path.
func (d *Dialer) dialQUIC(ctx context.Context, addr, path string, mux *TrackMux) (*Session, error) {
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

	// Native QUIC has no handshake-time request URI, so the client is the one
	// role that conveys the request path in its own SETUP. sendPath drives that.
	return newSession(conn, mux, nil, d.Config, d.FetchHandler, d.OnGoaway, d.Logger,
		sessionSetup{path: path, sendPath: true}, nil), nil
}
