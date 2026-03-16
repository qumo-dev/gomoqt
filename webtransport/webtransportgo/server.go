package webtransportgo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/okdaichi/gomoqt/quic"
	"github.com/okdaichi/gomoqt/webtransport"
	quicgo_quicgo "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

var _ webtransport.Server = (*Server)(nil)

// Server is a wrapper for (quic-go/webtransport-go).Server
type Server struct {
	CheckOrigin          func(r *http.Request) bool
	ApplicationProtocols []string
	internalServer       *quicgo_webtransportgo.Server
	initOnce             sync.Once
}

func (s *Server) init() {
	s.initOnce.Do(func() {
		if s.internalServer == nil {
			s.internalServer = &quicgo_webtransportgo.Server{
				H3:                   &http3.Server{},
				CheckOrigin:          s.CheckOrigin,
				ApplicationProtocols: s.ApplicationProtocols,
			}
		}
	})
}

func (s *Server) Upgrade(w http.ResponseWriter, r *http.Request) (quic.Connection, error) {
	s.init()
	wtsess, err := s.internalServer.Upgrade(w, r)
	if err != nil {
		return nil, err
	}

	return wrapSession(wtsess), nil
}

func (s *Server) ServeQUICConn(conn quic.Connection) error {
	s.init()
	if conn == nil {
		return nil
	}
	if wrapper, ok := conn.(quicgoUnwrapper); ok {
		return s.internalServer.ServeQUICConn(wrapper.Unwrap())
	}
	return errors.New("invalid connection type: expected a wrapped quic-go connection with Unwrap() method")
}

type quicgoUnwrapper interface {
	Unwrap() *quicgo_quicgo.Conn
}

func (s *Server) Serve(conn net.PacketConn) error {
	s.init()

	return s.internalServer.Serve(conn)
}

func (s *Server) Close() error {
	if s.internalServer != nil {
		return s.internalServer.Close()
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Implement a proper shutdown logic that passes the context to the server
	closeCh := make(chan struct{})

	// Close the server in a separate goroutine
	go func() {
		if s.internalServer != nil {
			_ = s.internalServer.Close() // Ignore close error as server is shutting down
		}
		close(closeCh)
	}()

	// Wait for either the context to be done or the close to complete
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-closeCh:
		return nil
	}
}
