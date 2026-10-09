// Package qmuxgo runs MOQ sessions over QMux on WebSocket, for clients that
// cannot use WebTransport. It adapts github.com/okdaichi/qmux-go to the
// transport interfaces.
package qmuxgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/okdaichi/qmux-go/qmux"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// Version is the QMux draft spoken, as it prefixes the WebSocket
// subprotocol: "qmux-02.<application protocol>".
const Version = "qmux-02"

var _ transport.WebTransportSession = (*session)(nil)

// session presents a QMux connection as a WebTransport session: both carry
// QUIC's streams under an HTTP upgrade that settled the path and the
// application protocol.
type session struct {
	conn *qmux.Conn
	// protocol is the application protocol the subprotocol named.
	protocol string
	// tls is the state of the TLS connection under the WebSocket, or nil
	// over plain TCP.
	tls *tls.ConnectionState
}

func (s *session) AcceptStream(ctx context.Context) (transport.Stream, error) {
	stream, err := s.conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	stream, err := s.conn.AcceptUniStream(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) OpenStream() (transport.Stream, error) {
	stream, err := s.conn.OpenStream()
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	stream, err := s.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) OpenUniStream() (transport.SendStream, error) {
	stream, err := s.conn.OpenUniStream()
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	stream, err := s.conn.OpenUniStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *session) CloseWithError(code transport.ConnErrorCode, msg string) error {
	return s.conn.CloseWithError(code, msg)
}

func (s *session) Context() context.Context { return s.conn.Context() }
func (s *session) LocalAddr() net.Addr      { return s.conn.LocalAddr() }
func (s *session) RemoteAddr() net.Addr     { return s.conn.RemoteAddr() }
func (s *session) Subprotocol() string      { return s.protocol }

func (s *session) TLS() *tls.ConnectionState { return s.tls }

// ConnectionStats reports the bytes exchanged and, when the connection
// sends keep-alive pings, the round-trip time.
func (s *session) ConnectionStats() quic.ConnectionStats {
	return s.conn.ConnectionStats()
}
