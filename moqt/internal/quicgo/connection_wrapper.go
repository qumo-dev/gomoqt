package quicgo

import (
	"context"
	"crypto/tls"
	"net"

	quicgo_quicgo "github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

func wrapConnection(conn *quicgo_quicgo.Conn) transport.StreamConn {
	if conn == nil {
		return nil
	}
	return &connWrapper{
		conn: conn,
	}
}

var _ transport.StreamConn = (*connWrapper)(nil)

type connWrapper struct {
	conn *quicgo_quicgo.Conn
}

func (wrapper *connWrapper) AcceptStream(ctx context.Context) (transport.Stream, error) {
	stream, err := wrapper.conn.AcceptStream(ctx)
	return &rawQuicStream{stream: stream}, err
}

func (wrapper *connWrapper) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	stream, err := wrapper.conn.AcceptUniStream(ctx)
	return &rawQuicReceiveStream{stream: stream}, err
}

func (wrapper *connWrapper) CloseWithError(code transport.ConnErrorCode, msg string) error {
	return wrapper.conn.CloseWithError(code, msg)
}

func (wrapper *connWrapper) TLS() *tls.ConnectionState {
	state := wrapper.conn.ConnectionState()
	return &state.TLS
}

func (wrapper *connWrapper) Context() context.Context {
	return wrapper.conn.Context()
}

func (wrapper *connWrapper) LocalAddr() net.Addr {
	return wrapper.conn.LocalAddr()
}

func (wrapper *connWrapper) OpenStream() (transport.Stream, error) {
	stream, err := wrapper.conn.OpenStream()
	return &rawQuicStream{stream: stream}, err
}

func (wrapper *connWrapper) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	stream, err := wrapper.conn.OpenStreamSync(ctx)
	return &rawQuicStream{stream: stream}, err
}

func (wrapper *connWrapper) OpenUniStream() (transport.SendStream, error) {
	stream, err := wrapper.conn.OpenUniStream()
	return &rawQuicSendStream{stream: stream}, err
}

func (wrapper *connWrapper) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	stream, err := wrapper.conn.OpenUniStreamSync(ctx)
	return &rawQuicSendStream{stream: stream}, err
}

func (wrapper *connWrapper) RemoteAddr() net.Addr {
	return wrapper.conn.RemoteAddr()
}

func (wrapper *connWrapper) QUICConn() *quicgo_quicgo.Conn {
	return wrapper.conn
}

func (wrapper *connWrapper) ConnectionStats() quicgo_quicgo.ConnectionStats {
	return wrapper.conn.ConnectionStats()
}
