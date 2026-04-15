package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/moqt/internal/quicgo"
	"github.com/okdaichi/gomoqt/moqt/internal/webtransportgo"
	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
)

// ListenAndServe starts a new Server bound to the specified address and TLS
// configuration. It is a convenience helper that constructs a Server with the
// provided settings and calls its ListenAndServe method.
func ListenAndServe(addr string, tlsConfig *tls.Config) error {
	server := &Server{
		Addr:      addr,
		TLSConfig: tlsConfig,
	}
	return server.ListenAndServe()
}

type WebTransportServer interface {
	ServeQUICConn(conn StreamConn) error
	Close() error
}

// Server is a MOQ server that accepts both WebTransport and native QUIC
// connections. It handles session setup, track announcements, and subscriptions
// according to the MOQ Lite specification.
//
// The server maintains active sessions and listeners and provides graceful
// shutdown capabilities.
type Server struct {
	// Address to listen on, in the form "host:port".
	Addr string

	// TLS configuration
	TLSConfig *tls.Config

	// QUIC configuration
	QUICConfig *quic.Config

	// MoQ configuration
	Config *Config

	// ListenFunc is a function that creates a new QUIC listener
	// If nil, the server will use quic.ListenAddrEarly from the quic-go library.
	ListenFunc func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (QUICListener, error)

	// WebTransport server for handling WebTransport sessions.
	// If nil, the server will use a default implementation.
	WebTransportServer WebTransportServer

	// TrackMux is used for routing announcements and track subscriptions.
	// If nil, the server should use a global default mux or initialize a new one.
	TrackMux *TrackMux

	// Handler serves accepted native QUIC sessions (i.e. connections negotiated with NextProtoMOQ).
	// If nil, native QUIC connections are not handled.
	Handler Handler

	// FetchHandler serves incoming FETCH requests on native QUIC sessions.
	// If nil, FETCH requests are rejected with an internal stream error.
	FetchHandler FetchHandler

	// Logger for server events and errors. Optional; if nil, logging is disabled.
	Logger *slog.Logger

	// NextSessionURI is the URI sent to clients during Shutdown, allowing them
	// to reconnect to a different server. If empty, no redirect URI is provided.
	NextSessionURI string

	ConnContext func(ctx context.Context, conn StreamConn) context.Context

	listenerMu    sync.RWMutex
	listeners     map[QUICListener]struct{}
	listenerGroup sync.WaitGroup

	connManager *connManager

	initOnce sync.Once

	inShutdown atomic.Bool
}

func (s *Server) init() {
	s.initOnce.Do(func() {
		s.listeners = make(map[QUICListener]struct{})
		s.connManager = newConnManager()
		if s.WebTransportServer == nil {
			s.WebTransportServer = &webtransportgo.Server{}
		}
	})
}

type serverContextKeyType struct{}

var serverContextKey = serverContextKeyType{}

// ServeQUICListener accepts connections on the provided QUIC listener and handles them using the Server's configuration.
// This runs until the listener is closed or the server shuts down.
func (s *Server) ServeQUICListener(ln QUICListener) error {
	if s.shuttingDown() {
		return ErrServerClosed
	}

	s.init()

	s.addListener(ln)
	defer s.removeListener(ln)

	// Create context for listener's Accept operation
	// This context will be canceled when the server is shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for shutdown and cancel context when shutting down
	go func() {
		for !s.shuttingDown() {
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
	}()

	for {
		// Listen for new QUIC connections
		conn, err := ln.Accept(ctx)
		if err != nil {
			// Check if this is due to shutdown
			if s.shuttingDown() {
				return ErrServerClosed
			}
			// Check if context was cancelled
			if errors.Is(err, context.Canceled) {
				return ErrServerClosed
			}
			return fmt.Errorf("failed to accept QUIC connection: %w", err)
		}

		// Handle connection in a goroutine
		go func(conn StreamConn) {
			_ = s.ServeQUICConn(conn)
		}(conn)
	}
}

// ServeQUICConn serves a single QUIC connection.
// It detects whether the connection uses WebTransport or the native MOQ ALPN and dispatches to the appropriate handling logic for the session.
func (s *Server) ServeQUICConn(conn StreamConn) error {
	if s.shuttingDown() {
		return ErrServerClosed
	}

	s.init()

	tlsInfo := conn.TLS()
	if tlsInfo == nil {
		return fmt.Errorf("connection does not have TLS information; cannot determine protocol")
	}
	switch protocol := tlsInfo.NegotiatedProtocol; protocol {
	case NextProtoH3:
		ctx := s.connContext(conn.Context(), conn)
		wrapped := &streamConnContext{StreamConn: conn, ctx: ctx}
		return s.WebTransportServer.ServeQUICConn(wrapped)
	case NextProtoMOQ:
		return s.handleNativeQUIC(conn)
	default:
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

func (s *Server) connContext(ctx context.Context, conn StreamConn) context.Context {
	ctx = context.WithValue(ctx, serverContextKey, s.connManager)

	if s.ConnContext != nil {
		custom := s.ConnContext(ctx, conn)
		if custom == nil {
			panic("ConnContext returned nil")
		}
		return custom
	}
	return ctx
}

// streamConnContext wraps a StreamConn to override its Context.
// It also delegates QUICConn() to the inner connection so that
// webtransportgo.Server can extract the raw *quic.Conn.
type streamConnContext struct {
	StreamConn
	ctx context.Context
}

func (w *streamConnContext) Context() context.Context {
	return w.ctx
}

func (w *streamConnContext) QUICConn() *quic.Conn {
	type quicConnProvider interface {
		QUICConn() *quic.Conn
	}
	if p, ok := w.StreamConn.(quicConnProvider); ok {
		return p.QUICConn()
	}
	return nil
}

type WebTransportHandler struct {
	Config   *Config
	TrackMux *TrackMux

	// CheckOrigin validates the origin of an incoming upgrade request.
	// If nil, defaults may allow all origins (behavior defined by upgrader implementation).
	CheckOrigin func(r *http.Request) bool

	// ApplicationProtocols lists ALPN tokens supported for WebTransport upgrades.
	// If empty, MOQ's default protocol (NextProtoMOQ) is used.
	ApplicationProtocols []string

	// ReorderingTimeout sets the maximum wait time for out-of-order packets in WebTransport streams.
	// Zero means default behavior for the underlying transport stack.
	ReorderingTimeout time.Duration

	// Handler handles the accepted WebTransport session after successful handshake.
	Handler Handler

	// FetchHandler handles incoming fetch requests on WebTransport sessions. Optional; when nil, fetch requests are not handled.
	FetchHandler FetchHandler

	// UpgradeFunc performs a custom upgrade from HTTP request to QUIC StreamConn.
	// If nil, the default WebTransport upgrader is used.
	UpgradeFunc func(w http.ResponseWriter, r *http.Request) (WebTransportSession, error)

	// FallbackHandler handles non-WebTransport requests (e.g., plain HTTP on the same endpoint).
	// Optional; when nil, behavior is determined by the server’s default request handling.
	FallbackHandler http.Handler

	// Logger for WebTransport events and errors. Optional; if nil, logging is disabled.
	Logger *slog.Logger
}

func (u *WebTransportHandler) upgradeWebTransport(w http.ResponseWriter, r *http.Request) (WebTransportSession, error) {
	if u.UpgradeFunc != nil {
		return u.UpgradeFunc(w, r)
	}
	protocols := u.ApplicationProtocols
	if len(protocols) == 0 {
		protocols = []string{NextProtoMOQ}
	}
	// Fallback to default upgrader if custom upgrader is not set
	defaultUpgrader := webtransportgo.Upgrader{
		CheckOrigin:          u.CheckOrigin,
		ApplicationProtocols: protocols,
		ReorderingTimeout:    u.ReorderingTimeout,
	}
	return defaultUpgrader.Upgrade(w, r)
}

// ServeHTTP upgrades an incoming HTTP request to a WebTransport session and
// dispatches it to the configured handler. If the upgrade fails, it falls back
// to FallbackHandler or returns a 400 response.
func (u *WebTransportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := u.upgradeWebTransport(w, r)
	if err != nil {
		u.fallback(w, r)
		return
	}

	// When WebTransportHandler is used standalone (not via Server),
	// the context does not contain a connManager.
	var manager *connManager
	if v := r.Context().Value(serverContextKey); v != nil {
		manager = v.(*connManager)
	}

	sess := newSession(conn, u.TrackMux, manager, u.FetchHandler, nil, u.Logger)

	u.Handler.ServeMOQ(sess)
}

func (h *WebTransportHandler) fallback(w http.ResponseWriter, r *http.Request) {
	if h.FallbackHandler != nil {
		h.FallbackHandler.ServeHTTP(w, r)
	} else {
		http.Error(w, "fallback handler not configured", http.StatusBadRequest)
	}
}

type Handler interface {
	ServeMOQ(sess *Session)
}

type HandleFunc func(sess *Session)

func (f HandleFunc) ServeMOQ(sess *Session) {
	f(sess)
}

func (s *Server) handleNativeQUIC(conn StreamConn) error {
	if s.Handler != nil {
		sess := newSession(conn, s.TrackMux, s.connManager, s.FetchHandler, nil, s.Logger)
		s.Handler.ServeMOQ(sess)
	}
	return fmt.Errorf("no native QUIC handler configured")
}

// ListenAndServe starts the server by listening on the server's Address and serving QUIC connections.
// TLS configuration must be provided on the Server for ListenAndServe to function properly.
func (s *Server) ListenAndServe() error {
	s.init()

	// Configure TLS for QUIC
	if s.TLSConfig == nil {
		return fmt.Errorf("configuration for TLS is required for QUIC")
	}

	// Clone the TLS config to avoid modifying the original
	tlsConfig := s.TLSConfig.Clone()

	// Make sure we have NextProtos set for ALPN negotiation
	if len(tlsConfig.NextProtos) == 0 {
		tlsConfig.NextProtos = []string{NextProtoH3, NextProtoMOQ}
	}

	// Ensure WebTransport required QUIC flags are enabled.
	var quicConf *quic.Config
	if s.QUICConfig == nil {
		quicConf = &quic.Config{}
	} else {
		quicConf = s.QUICConfig.Clone()
	}
	quicConf.EnableDatagrams = true
	quicConf.EnableStreamResetPartialDelivery = true

	listenFunc := s.ListenFunc
	if listenFunc == nil {
		listenFunc = quicgo.ListenAddrEarly
	}
	ln, err := listenFunc(s.Addr, tlsConfig, quicConf)
	if err != nil {
		return fmt.Errorf("failed to start QUIC listener at %s: %w", s.Addr, err)
	}

	return s.ServeQUICListener(ln)
}

// ListenAndServeTLS starts the listener over QUIC/TLS using the provided
// certificate files. It wraps ListenAndServe by creating a TLS config from
// the provided cert/key files.
func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	if s.shuttingDown() {
		return ErrServerClosed
	}
	s.init()

	var err error
	// Generate TLS configuration
	certs := make([]tls.Certificate, 1)
	certs[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load X509 key pair (cert=%s, key=%s): %w", certFile, keyFile, err)
	}

	// Create TLS config with certificates
	tlsConfig := &tls.Config{
		Certificates: certs,
		NextProtos:   []string{NextProtoH3, NextProtoMOQ},
	}

	// Ensure WebTransport required QUIC flags are enabled.
	var quicConf *quic.Config
	if s.QUICConfig == nil {
		quicConf = &quic.Config{}
	} else {
		quicConf = s.QUICConfig.Clone()
	}
	quicConf.EnableDatagrams = true
	quicConf.EnableStreamResetPartialDelivery = true

	listenFunc := s.ListenFunc
	if listenFunc == nil {
		listenFunc = quicgo.ListenAddrEarly
	}

	ln, err := listenFunc(s.Addr, tlsConfig.Clone(), quicConf)
	if err != nil {
		return err
	}

	return s.ServeQUICListener(ln)
}

// Close gracefully shuts down the server by closing all listeners and
// sessions, waiting until all sessions have been terminated.
func (s *Server) Close() error {
	if s.shuttingDown() {
		return ErrServerClosed
	}

	// Set the shutdown flag
	s.inShutdown.Store(true)

	// Ensure that the server is initialized
	s.init()

	// Close all listeners first to stop accepting new connections
	s.listenerMu.Lock()
	for ln := range s.listeners {
		ln.Close()
	}
	s.listenerMu.Unlock()

	connectionManager := s.connManager
	s.connManager = nil

	// Terminate all active sessions
	for conn := range connectionManager.connections {
		// Close sessions concurrently; log potential errors.
		go func(conn StreamConn) {

		}(conn)
	}

	// Wait for all sessions to close
	<-connectionManager.Done()

	// Close WebTransport server (guard against panics from underlying implementations)
	if s.WebTransportServer != nil {
		done := make(chan struct{})
		go func() {
			defer func() { recover() }()
			_ = s.WebTransportServer.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			// timed out waiting for Close; proceed
		}
	}

	// Wait for listener goroutines to exit so removeListener can call Done().
	s.listenerGroup.Wait()

	// Clear listeners map
	s.listenerMu.Lock()
	s.listeners = nil
	s.listenerMu.Unlock()

	return nil
}

// Shutdown gracefully shuts down the server.
// It stops accepting new connections, asks active connections to go away,
// and waits for all tracked connections to close.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.shuttingDown() {
		return ErrServerClosed
	}

	// Set the shutdown flag
	s.inShutdown.Store(true)

	// Close all listeners first to stop accepting new connections
	s.listenerMu.Lock()
	for ln := range s.listeners {
		ln.Close()
	}
	s.listenerMu.Unlock()

	connManager := s.connManager
	s.connManager = nil

	for conn := range connManager.connections {
		// Send goaway to sessions concurrently; log potential errors.
		go func(conn StreamConn) {
			err := s.goAway(ctx, conn)
			if logger := s.Logger; logger != nil && err != nil {
				logger.Error("error sending GOAWAY to connection during shutdown", "error", err)
			}
		}(conn)
	}

	// Wait for all sessions to close
	<-connManager.Done()

	// Close WebTransport server (guard against panics from underlying implementations)
	if s.WebTransportServer != nil {
		done := make(chan struct{})
		go func() {
			defer func() { recover() }()
			_ = s.WebTransportServer.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			// timed out waiting for Close; proceed
		}
	}

	// Wait for listener goroutines to exit so removeListener can call Done().
	s.listenerGroup.Wait()

	// Clear listeners map
	s.listenerMu.Lock()
	s.listeners = nil
	s.listenerMu.Unlock()

	return nil
}

// goAway sends a GOAWAY message on a new bidirectional stream and then waits
// for the connection to close naturally or the shutdown context to expire,
// closing the connection with a timeout error if needed.
func (s *Server) goAway(ctx context.Context, conn StreamConn) error {
	// Best-effort attempt to send a GOAWAY message.
	stream, err := conn.OpenStream()
	if err != nil {
		return err
	}
	defer stream.Close()

	err = message.StreamTypeGoaway.Encode(stream)
	if err != nil {
		return err
	}
	err = message.GoawayMessage{NewSessionURI: s.NextSessionURI}.Encode(stream)
	if err != nil {
		return err
	}

	select {
	case <-conn.Context().Done():
		// Connection already closed; nothing to do
	case <-ctx.Done():
		// Context canceled, close connection with error
		conn.CloseWithError(transport.ConnErrorCode(GoAwayTimeoutErrorCode), GoAwayTimeoutErrorCode.String())
	}

	return nil
}

func (s *Server) addListener(ln QUICListener) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	if s.listeners == nil {
		s.listeners = make(map[QUICListener]struct{})
	}
	s.listeners[ln] = struct{}{}
	s.listenerGroup.Add(1)
}

func (s *Server) removeListener(ln QUICListener) {
	s.listenerMu.Lock()

	_, ok := s.listeners[ln]
	if ok {
		delete(s.listeners, ln)
	}

	s.listenerMu.Unlock()

	if ok {
		s.listenerGroup.Done()
	}
}

func (s *Server) shuttingDown() bool {
	return s.inShutdown.Load()
}
