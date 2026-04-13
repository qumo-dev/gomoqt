package webtransportgo

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	quicgo_quicgo "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Server is a wrapper for (quic-go/webtransport-go).Server
type Server struct {
	internalServer *quicgo_webtransportgo.Server
	connContexts   sync.Map // *quicgo_quicgo.Conn -> context.Context
	initOnce       sync.Once
}

func (s *Server) init() {
	s.initOnce.Do(func() {
		if s.internalServer == nil {
			s.internalServer = &quicgo_webtransportgo.Server{
				H3: &http3.Server{},
			}
		}
		if s.internalServer.H3 == nil {
			s.internalServer.H3 = &http3.Server{}
		}
		s.internalServer.H3.ConnContext = func(ctx context.Context, c *quicgo_quicgo.Conn) context.Context {
			if stored, ok := s.connContexts.Load(c); ok {
				return stored.(context.Context)
			}
			return ctx
		}
	})
}

func (s *Server) ServeQUICConn(conn transport.StreamConn) error {
	s.init()
	if conn == nil {
		return nil
	}
	if provider, ok := conn.(quicConnProvider); ok {
		qc := provider.QUICConn()
		s.connContexts.Store(qc, conn.Context())
		defer s.connContexts.Delete(qc)
		return s.internalServer.ServeQUICConn(qc)
	}
	return errors.New("invalid connection type: expected QUICConn() method")
}

type quicConnProvider interface {
	QUICConn() *quicgo_quicgo.Conn
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
