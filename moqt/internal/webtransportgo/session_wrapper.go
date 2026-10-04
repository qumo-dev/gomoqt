package webtransportgo

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

type sessionWrapper struct {
	sess *quicgo_webtransportgo.Session
}

func wrapSession(wtsess *quicgo_webtransportgo.Session) transport.WebTransportSession {
	if wtsess == nil {
		return nil
	}
	return &sessionWrapper{
		sess: wtsess,
	}
}

// CloseCause converts the error a WebTransport session was closed with, the
// cause its context ends with (a *SessionError with the code and message,
// local or from the peer's WT_CLOSE_SESSION capsule), into the
// transport.ApplicationError a native QUIC connection's context ends with.
// Any other error is returned as is.
func CloseCause(err error) error {
	if sessErr, ok := errors.AsType[*quicgo_webtransportgo.SessionError](err); ok {
		return &transport.ApplicationError{
			ErrorCode:    transport.ApplicationErrorCode(sessErr.ErrorCode),
			ErrorMessage: sessErr.Message,
			Remote:       sessErr.Remote,
		}
	}
	return err
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
