package webtransportgo

import (
	"context"
	"crypto/tls"
	"net"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

type sessionWrapper struct {
	sess  *quicgo_webtransportgo.Session
	qconn *quic.Conn      // the QUIC connection under a dialed session, else nil
	qtr   *quic.Transport // the transport it was dialed from, else nil
}

// QUICTransport is the transport a dialed session's QUIC connection was
// dialed from, or nil for a server's.
func (conn *sessionWrapper) QUICTransport() *quic.Transport {
	return conn.qtr
}

// QUICConn is the QUIC connection under a dialed session, or nil for a
// server's.
func (conn *sessionWrapper) QUICConn() *quic.Conn {
	return conn.qconn
}

func wrapSession(wtsess *quicgo_webtransportgo.Session) transport.WebTransportSession {
	if wtsess == nil {
		return nil
	}
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

func (conn *sessionWrapper) RemoteAddr() net.Addr {
	return conn.sess.RemoteAddr()
}

func (conn *sessionWrapper) Subprotocol() string {
	return conn.sess.SessionState().ApplicationProtocol
}

func (conn *sessionWrapper) ConnectionStats() quic.ConnectionStats {
	return conn.sess.ConnectionStats()
}
