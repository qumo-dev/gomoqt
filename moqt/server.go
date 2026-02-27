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
	"github.com/okdaichi/gomoqt/quic"
	"github.com/okdaichi/gomoqt/quic/quicgo"
	"github.com/okdaichi/gomoqt/webtransport"
	"github.com/okdaichi/gomoqt/webtransport/webtransportgo"
)

// ListenAndServe starts a new Server bound to the specified address and TLS configuration and runs it until an error occurs.
// This is a convenience helper that constructs a Server with the default setup handler and calls its ListenAndServe method.
func ListenAndServe(addr string, tlsConfig *tls.Config) error {
	server := &Server{
		Addr:         addr,
		TLSConfig:    tlsConfig,
		SetupHandler: DefaultRouter,
	}
	return server.ListenAndServe()
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
	 * Set-up Request SetupHandler
	 */
	SetupHandler SetupHandler

	/*
	 * Listen QUIC function
	 */
	ListenFunc quic.ListenAddrFunc

	/*
	 * WebTransport Server
	 * If the server is configured with a WebTransport server, it is used to handle WebTransport sessions.
	 * If not, a default server is used.
	 */
	NewWebtransportServerFunc func(checkOrigin func(*http.Request) bool) webtransport.Server
	wtServer                  webtransport.Server

	// CheckHTTPOrigin validates the HTTP Origin header for WebTransport connections.
	// If nil, all origins are accepted.
	CheckHTTPOrigin func(*http.Request) bool

	/*
	 * Logger
	 */
	Logger *slog.Logger

	listenerMu    sync.RWMutex
	listeners     map[quic.Listener]struct{}
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
		s.listeners = make(map[quic.Listener]struct{})
		s.doneChan = make(chan struct{})
		s.activeSess = make(map[*Session]struct{})
		// Initialize WebtransportServer
		// Use Server-level CheckHTTPOrigin. Config no longer holds origin checks.
		checkOrigin := s.CheckHTTPOrigin

		if s.NewWebtransportServerFunc != nil {
			s.wtServer = s.NewWebtransportServerFunc(checkOrigin)
		} else {
			s.wtServer = webtransportgo.NewServer(checkOrigin)
		}
	})
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
func (s *Server) ServeQUICListener(ln quic.Listener) error {
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
		go func(conn quic.Connection) {
			_ = s.ServeQUICConn(conn)
		}(conn)
	}
}

// ServeQUICConn serves a single QUIC connection.
// It detects whether the connection uses WebTransport or the native MOQ ALPN and dispatches to the appropriate handling logic for the session.
func (s *Server) ServeQUICConn(conn quic.Connection) error {
	if s.shuttingDown() {
		return ErrServerClosed
	}

	s.init()

	switch protocol := conn.ConnectionState().TLS.NegotiatedProtocol; protocol {
	case NextProtoH3:
		return s.wtServer.ServeQUICConn(conn)
	case NextProtoMOQ:
		return s.handleNativeQUIC(conn)
	default:
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// HandleWebTransport upgrades an incoming HTTP request to a WebTransport
// connection and handles session handshake and setup using the Server's
// SetupHandler.
func (s *Server) HandleWebTransport(w http.ResponseWriter, r *http.Request) error {
	if s.shuttingDown() {
		return fmt.Errorf("server is shutting down")
	}

	s.init()

	if r.TLS == nil {
		s.log().Warn("connection rejected: plain HTTP is not supported; use HTTPS",
			"remote_address", r.RemoteAddr,
		)
		http.Error(w, "plain HTTP is not supported; use HTTPS", http.StatusUpgradeRequired)
		return fmt.Errorf("plain HTTP connection from %s", r.RemoteAddr)
	}

	conn, err := s.wtServer.Upgrade(w, r)
	if err != nil {
		return fmt.Errorf("failed to upgrade connection: %w", err)
	}

	acceptCtx, cancelAccept := context.WithTimeout(r.Context(), s.Config.setupTimeout())
	defer cancelAccept()
	req, rsp, err := acceptSessionStream(acceptCtx, conn)
	if err != nil {
		return fmt.Errorf("failed to accept session stream: %w", err)
	}

	// Use the path in the HTTP request as the MoQ setup request
	req.Path = r.URL.Path

	responseWriter := newResponseWriter(conn, rsp, s, req.Versions)

	return s.setupAndServe(responseWriter, req)
}

func (s *Server) handleNativeQUIC(conn quic.Connection) error {
	if s.shuttingDown() {
		return nil
	}

	s.init()

	acceptCtx, cancelAccept := context.WithTimeout(conn.Context(), s.Config.setupTimeout())
	defer cancelAccept()
	req, rsp, err := acceptSessionStream(acceptCtx, conn)
	if err != nil {
		return fmt.Errorf("moq: failed to accept session stream: %w", err)
	}

	w := newResponseWriter(conn, rsp, s, req.Versions)

	return s.setupAndServe(w, req)
}

// setupAndServe dispatches the setup request to the configured handler, recovering
// from any panic the handler may raise and logging it as a server-level error.
func (s *Server) setupAndServe(w SetupResponseWriter, req *SetupRequest) error {
	var err error
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic in setup handler: %v", rec)
			s.log().Error("panic in setup handler", "panic", rec)
			w.Reject(SetupFailedErrorCode)
		}
	}()

	if s.SetupHandler != nil {
		s.SetupHandler.ServeMOQ(w, req)
	} else {
		DefaultRouter.ServeMOQ(w, req)
	}

	return err
}

func acceptSessionStream(acceptCtx context.Context, conn quic.Connection) (*SetupRequest, *response, error) {
	stream, err := conn.AcceptStream(acceptCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to accept a session stream: %w", err)
	}

	var streamType message.StreamType
	err = streamType.Decode(stream)
	if err != nil {
		var appErr *quic.ApplicationError
		if errors.As(err, &appErr) {
			return nil, nil, &SessionError{ApplicationError: appErr}
		} else {
			return nil, nil, fmt.Errorf("moq: failed to receive STREAM_TYPE message: %w", err)
		}
	}

	var scm message.SessionClientMessage
	err = scm.Decode(stream)
	if err != nil {
		var appErr *quic.ApplicationError
		if errors.As(err, &appErr) {
			return nil, nil, &SessionError{ApplicationError: appErr}
		}

		return nil, nil, err
	}

	// Get the client parameters
	clientParams := &Extension{scm.Parameters}

	// Get the path parameter
	path, _ := clientParams.GetString(param_type_path)

	versions := make([]Version, len(scm.SupportedVersions))
	for i, v := range scm.SupportedVersions {
		versions[i] = Version(v)
	}

	r := &SetupRequest{
		reqCtx:           stream.Context(),
		Path:             path,
		Versions:         versions,
		ClientExtensions: clientParams,
		RemoteAddr:       conn.RemoteAddr().String(),
	}

	rsp := newResponse(newSessionStream(stream), DefaultServerVersion, NewExtension())

	return r, rsp, nil
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
		tlsConfig.NextProtos = []string{NextProtoMOQ}
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

	var ln quic.Listener
	var err error
	if s.ListenFunc != nil {
		ln, err = s.ListenFunc(s.Addr, tlsConfig, quicConf)
	} else {
		ln, err = quicgo.ListenAddrEarly(s.Addr, tlsConfig, quicConf)
	}
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
		NextProtos:   []string{NextProtoMOQ, webtransport.NextProtoH3},
	}

	// Ensure WebTransport required QUIC flags are enabled.
	quicConf := s.QUICConfig
	if quicConf == nil {
		quicConf = &quic.Config{}
	} else {
		clone := *quicConf
		quicConf = &clone
	}
	quicConf.EnableDatagrams = true
	quicConf.EnableStreamResetPartialDelivery = true

	var ln quic.Listener
	if s.ListenFunc != nil {
		ln, err = s.ListenFunc(s.Addr, tlsConfig.Clone(), quicConf)
	} else {
		ln, err = quicgo.ListenAddrEarly(s.Addr, tlsConfig.Clone(), quicConf)
	}
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
	if s.wtServer != nil {
		done := make(chan struct{})
		go func() {
			defer func() { recover() }()
			_ = s.wtServer.Close()
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
	if s.wtServer != nil {
		done := make(chan struct{})
		go func() {
			defer func() { recover() }()
			_ = s.wtServer.Close()
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

func (s *Server) addListener(ln quic.Listener) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()

	if s.listeners == nil {
		s.listeners = make(map[quic.Listener]struct{})
	}
	s.listeners[ln] = struct{}{}
	s.listenerGroup.Add(1)
}

func (s *Server) removeListener(ln quic.Listener) {
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
