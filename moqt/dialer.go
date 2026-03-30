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

type DialQUICFunc func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error)

type DialWebTransportFunc func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, StreamConn, error)

// Dialer is a MOQ client that can establish sessions with MOQ servers.
// It supports both WebTransport (for browser compatibility) and raw QUIC connections.
//
// A Dialer can dial multiple servers and maintain multiple active sessions.
// Sessions are tracked and managed automatically. When the client shuts down,
// all active sessions are terminated gracefully.
type Dialer struct {
	/*
	 * TLS configuration
	 */
	TLSConfig *tls.Config

	/*
	 * QUIC configuration
	 */
	QUICConfig *quic.Config

	/*
	 * MOQ Configuration
	 */
	Config *Config

	/*
	 * Dial QUIC function
	 */
	DialQUICFunc DialQUICFunc

	/*
	 * Dial WebTransport function
	 */
	DialWebTransportFunc DialWebTransportFunc

	/*
	 * Logger
	 */
	Logger *slog.Logger
}

// log returns Client.Logger if set, otherwise a discard logger.
func (c *Dialer) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Dial establishes a new session to the specified URL using either WebTransport (https scheme) or QUIC (moqt scheme).
// The provided TrackMux is used to route incoming service tracks if non-nil.
// Dial returns the newly created Session or an error.
func (c *Dialer) Dial(ctx context.Context, urlStr string, mux *TrackMux) (*Session, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Dial based on the scheme
	switch parsedURL.Scheme {
	case "https":
		return c.DialWebTransport(ctx, parsedURL.Host, parsedURL.Path, mux)
	case "moqt":
		return c.DialQUIC(ctx, parsedURL.Host, parsedURL.Path, mux)
	default:
		return nil, ErrInvalidScheme
	}
}

// DialWebTransport establishes a new session over WebTransport (HTTP/3).
// It performs the WebTransport handshake and initializes a MOQ session.
// `host` should be host:port and `path` is the path used for session setup.
func (c *Dialer) DialWebTransport(ctx context.Context, host, path string, mux *TrackMux) (*Session, error) {
	var baseLogger *slog.Logger
	if c.Logger != nil {
		baseLogger = c.Logger
	} else {
		baseLogger = slog.New(slog.DiscardHandler)
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, c.Config.setupTimeout())
	defer cancelDial()

	var dialer DialWebTransportFunc
	if c.DialWebTransportFunc != nil {
		dialer = c.DialWebTransportFunc
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

	_, conn, err := dialer(dialCtx, target, http.Header{}, c.TLSConfig)
	if err != nil {
		return nil, err
	}

	connLogger := baseLogger.With(
		"transport", "webtransport",
		"local_address", conn.LocalAddr(),
		"remote_address", conn.RemoteAddr(),
	)
	connLogger.Info("connection established")

	return newSession(conn, mux, nil), nil
}

// TODO: Expose this method if QUIC is supported
// DialQUIC establishes a new session over native QUIC by dialing the provided
// address and negotiating the transport protocol. This uses the QUIC dial
// function configured on the Client (DialQUICFunc) if present.
func (c *Dialer) DialQUIC(ctx context.Context, addr, path string, mux *TrackMux) (*Session, error) {
	dialTimeout := c.Config.setupTimeout()
	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	defer cancelDial()

	tlsConfig := c.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if len(tlsConfig.NextProtos) == 0 {
		tlsConfig.NextProtos = []string{NextProtoMOQ}
	}

	var dialFunc DialQUICFunc
	if c.DialQUICFunc != nil {
		dialFunc = c.DialQUICFunc
	} else {
		dialFunc = quicgo.DialAddrEarly
	}
	conn, err := dialFunc(dialCtx, addr, tlsConfig, c.QUICConfig)
	if err != nil {
		return nil, err
	}

	return newSession(conn, mux, nil), nil
}
