package webtransportgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

type sessionWrapper struct {
	sess *quicgo_webtransportgo.Session
}

func wrapSession(wtsess *quicgo_webtransportgo.Session) transport.StreamConn {
	return &sessionWrapper{
		sess: wtsess,
	}
}

func (conn *sessionWrapper) AcceptStream(ctx context.Context) (transport.Stream, error) {
	stream, err := conn.sess.AcceptStream(ctx)
	return &streamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	stream, err := conn.sess.AcceptUniStream(ctx)
	return &receiveStreamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) CloseWithError(code transport.ConnErrorCode, msg string) error {
	return conn.sess.CloseWithError(quicgo_webtransportgo.SessionErrorCode(code), msg)
}

type SessionState = quicgo_webtransportgo.SessionState

func (wrapper *sessionWrapper) TLS() *tls.ConnectionState {
	state := wrapper.sess.SessionState()
	return &state.ConnectionState.TLS
}

func (conn *sessionWrapper) Context() context.Context {
	return conn.sess.Context()
}

func (conn *sessionWrapper) LocalAddr() net.Addr {
	return conn.sess.LocalAddr()
}

func (conn *sessionWrapper) OpenStream() (transport.Stream, error) {
	stream, err := conn.sess.OpenStream()
	return &streamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	stream, err := conn.sess.OpenStreamSync(ctx)
	return &streamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) OpenUniStream() (transport.SendStream, error) {
	stream, err := conn.sess.OpenUniStream()
	return &sendStreamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	stream, err := conn.sess.OpenUniStreamSync(ctx)
	return &sendStreamWrapper{stream: stream}, err
}

func (conn *sessionWrapper) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	return conn.sess.ReceiveDatagram(ctx)
}

func (conn *sessionWrapper) RemoteAddr() net.Addr {
	return conn.sess.RemoteAddr()
}

func (conn *sessionWrapper) SendDatagram(b []byte) error {
	return conn.sess.SendDatagram(b)
}
