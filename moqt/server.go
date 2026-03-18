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

	"github.com/okdaichi/gomoqt/moqt/internal/quicgo"
	"github.com/okdaichi/gomoqt/moqt/internal/webtransportgo"
	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
)

// ListenAndServe starts a new Server bound to the specified address and TLS configuration and runs it until an error occurs.
// This is a convenience helper that constructs a Server with the default setup handler and calls its ListenAndServe method.
func ListenAndServe(addr string, tlsConfig *tls.Config) error {
	server := &Server{
		Addr:      addr,
		TLSConfig: tlsConfig,
	}
	return server.ListenAndServe()
}

type QUICListenFunc func(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.QUICListener, error)

type WebTransportServer interface {
	ServeQUICConn(conn transport.StreamConn) error
	Close() error
}

// Server is a MOQ server that accepts both WebTransport and raw QUIC connections.
// It handles session setup, track announcements, and subscriptions according to the MOQ Lite specification.
//
// The server maintains active sessions and listeners. It provides graceful shutdown capabilities and
// can serve over multiple listeners simultaneously.
type Server struct {
	/*
	 * Server's Address
	 */
	Addr string

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
	 * Listen QUIC function
	 */
	ListenFunc QUICListenFunc

	/*
	 * WebTransport Server
	 * If the server is configured with a WebTransport server, it is used to handle WebTransport sessions.
	 * If not, a default server is used.
	 */
	WebTransportServer WebTransportServer

	//
	NativeQUICHandler *NativeQUICHandler

	/*
	 * Logger
	 */
	Logger *slog.Logger

	ConnContext func(ctx context.Context, conn transport.StreamConn) context.Context

	listenerMu    sync.RWMutex
	listeners     map[transport.QUICListener]struct{}
	listenerGroup sync.WaitGroup

	sessMu     sync.RWMutex
	activeSess map[*Session]struct{}

	initOnce sync.Once

	inShutdown atomic.Bool

	// Channel to signal server shutdown completion
	// Closed when all sessions are closed
	doneChan chan struct{}
}

func (s *Server) init() {
	s.initOnce.Do(func() {
		s.listeners = make(map[transport.QUICListener]struct{})
		s.doneChan = make(chan struct{})
		s.activeSess = make(map[*Session]struct{})
		if s.WebTransportServer == nil {
			s.WebTransportServer = &webtransportgo.Server{ConnContext: s.connContext}
			return
		}
		if wtServer, ok := s.WebTransportServer.(*webtransportgo.Server); ok && wtServer.ConnContext == nil {
			wtServer.ConnContext = s.connContext
		}
	})
}

type serverContextKeyType struct{}

var moqServerContextKey = serverContextKeyType{}

func (s *Server) connContext(ctx context.Context, conn transport.StreamConn) context.Context {
	if s.ConnContext != nil {
		ctx = s.ConnContext(ctx, conn)
		if ctx == nil {
			panic("nil context returned by ConnContext")
		}
	}

	ctx = context.WithValue(ctx, moqServerContextKey, s)

	return ctx
}

// log returns Server.Logger if set, otherwise a discard logger.
func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// ServeQUICListener accepts connections on the provided QUIC listener and handles them using the Server's configuration.
// This runs until the listener is closed or the server shuts down.
func (s *Server) ServeQUICListener(ln transport.QUICListener) error {
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
		go func(conn transport.StreamConn) {
			_ = s.ServeQUICConn(conn)
		}(conn)
	}
}

// ServeQUICConn serves a single QUIC connection.
// It detects whether the connection uses WebTransport or the native MOQ ALPN and dispatches to the appropriate handling logic for the session.
func (s *Server) ServeQUICConn(conn transport.StreamConn) error {
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
		return s.WebTransportServer.ServeQUICConn(conn)
	case NextProtoMOQ:
		if s.NativeQUICHandler != nil {
			return s.NativeQUICHandler.handleNativeQUIC(conn)
		}
		return fmt.Errorf("native QUIC is not supported for protocol: %s", protocol)
	default:
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

type Upgrader struct {
	CheckOrigin          func(r *http.Request) bool
	ApplicationProtocols []string
	ReorderingTimeout    time.Duration
	TrackMux             *TrackMux
	UpgradeFunc          func(w http.ResponseWriter, r *http.Request) (transport.StreamConn, error)
}

func (u *Upgrader) upgradeWebTransport(w http.ResponseWriter, r *http.Request) (transport.StreamConn, error) {
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

// Upgrade upgrades an incoming HTTP request to a WebTransport session and registers it with the server's session management.
// It returns the established session or an error if the upgrade fails.
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*Session, error) {
	s, _ := r.Context().Value(moqServerContextKey).(*Server)

	if r.TLS == nil {
		if s != nil {
			s.log().Warn("connection rejected: plain HTTP is not supported; use HTTPS",
				"remote_address", r.RemoteAddr,
			)
		}
		http.Error(w, "plain HTTP is not supported; use HTTPS", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("plain HTTP connection from %s", r.RemoteAddr)
	}

	conn, err := u.upgradeWebTransport(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade connection: %w", err)
	}
	if s == nil {
		return newSession(conn, u.TrackMux, func() {}), nil
	}

	var sess *Session
	sess = newSession(conn, u.TrackMux, func() { s.removeSession(sess) })
	s.addSession(sess)

	return sess, nil
}

type NativeQUICHandler struct {
	TrackMux       *TrackMux
	SessionHandler func(sess *Session) error
}

func (h *NativeQUICHandler) handleNativeQUIC(conn transport.StreamConn) error {
	if h.SessionHandler != nil {
		return h.SessionHandler(newSession(conn, h.TrackMux, func() {})) // TODO: implement proper session cleanup callback
	}
	return fmt.Errorf("no session handler configured for native QUIC handler")
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

	var listenFunc QUICListenFunc
	if s.ListenFunc == nil {
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

	var listenFunc QUICListenFunc
	if s.ListenFunc == nil {
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

	// Terminate all active sessions
	s.sessMu.Lock()
	for sess := range s.activeSess {
		// Close sessions concurrently; log potential errors.
		go func(sess *Session) {
			_ = sess.CloseWithError(NoError, SessionErrorText(NoError))
		}(sess)
	}
	s.sessMu.Unlock()

	// Wait for all sessions to close
	// If there are no active sessions, close the doneChan now so Close doesn't block.
	s.sessMu.Lock()
	if len(s.activeSess) == 0 {
		select {
		case <-s.doneChan:
			// already closed
		default:
			close(s.doneChan)
		}
	}
	s.sessMu.Unlock()

	<-s.doneChan

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

// Shutdown gracefully shuts down the server. It stops accepting new
// connections, sends goaway to sessions and waits for active sessions to
// close, respecting the provided context for timeouts.
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

	// Send GOAWAY to all sessions
	s.goAway()

	// If there are no active sessions, close the doneChan now so shutdown returns immediately.
	s.sessMu.Lock()
	if len(s.activeSess) == 0 {
		select {
		case <-s.doneChan:
			// already closed
		default:
			close(s.doneChan)
		}
	}
	s.sessMu.Unlock()

	// Wait for sessions to close or context timeout
	select {
	case <-s.doneChan:
		// All sessions closed gracefully
	case <-ctx.Done():
		// Context canceled, terminate all sessions forcefully
		s.sessMu.Lock()
		for sess := range s.activeSess {
			go func(sess *Session) {
				_ = sess.CloseWithError(GoAwayTimeoutErrorCode, SessionErrorText(GoAwayTimeoutErrorCode))
			}(sess)
		}
		s.sessMu.Unlock()

		// Wait for all sessions to close
		<-s.doneChan
	}

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

func (s *Server) addListener(ln transport.QUICListener) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	if s.listeners == nil {
		s.listeners = make(map[transport.QUICListener]struct{})
	}
	s.listeners[ln] = struct{}{}
	s.listenerGroup.Add(1)
}

func (s *Server) removeListener(ln transport.QUICListener) {
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

func (s *Server) addSession(sess *Session) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()

	if sess == nil {
		return
	}
	s.activeSess[sess] = struct{}{}
}

func (s *Server) removeSession(sess *Session) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()

	if s.activeSess == nil {
		return
	}

	_, ok := s.activeSess[sess]
	if !ok {
		return
	}

	delete(s.activeSess, sess)

	// Send completion signal if connections reach zero and server is shutting down
	if len(s.activeSess) == 0 && s.shuttingDown() {
		// Close the done channel to signal server is done
		select {
		case <-s.doneChan:
			// Already closed
		default:
			close(s.doneChan)
		}
	}
}

func (s *Server) shuttingDown() bool {
	return s.inShutdown.Load()
}

func (s *Server) goAway() {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()

	for sess := range s.activeSess {
		_ = sess.goAway("") // TODO: specify URI if needed; log if required
	}
}
