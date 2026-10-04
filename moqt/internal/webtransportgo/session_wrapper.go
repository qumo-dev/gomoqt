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
	sess *quicgo_webtransportgo.Session
	ctx  context.Context
}

func wrapSession(wtsess *quicgo_webtransportgo.Session) transport.WebTransportSession {
	if wtsess == nil {
		return nil
	}
	// webtransport-go cancels its context without a cause. Its stream methods
	// retain the actual session error, including the peer's close capsule.
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(wtsess.Context()))
	context.AfterFunc(wtsess.Context(), func() {
		_, err := wtsess.OpenStreamSync(wtsess.Context())
		cancel(sessionCloseCause(err, context.Cause(wtsess.Context())))
	})
	return &sessionWrapper{sess: wtsess, ctx: ctx}
}

func sessionCloseCause(err, fallback error) error {
	if sessErr, ok := err.(*quicgo_webtransportgo.SessionError); ok {
		return &transport.ApplicationError{
			ErrorCode:    transport.ApplicationErrorCode(sessErr.ErrorCode),
			ErrorMessage: sessErr.Message,
			Remote:       sessErr.Remote,
		}
	}
	return fallback
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
	return conn.ctx
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
