package webtransportgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.StreamConn = (*fakeStreamConn)(nil)

// fakeStreamConn is a zero-configuration stub connection: every stream
// operation reports no stream and no error. Set ParentCtx to control the
// connection context; set the value fields to override an address or the TLS
// state.
type fakeStreamConn struct {
	parentCtx context.Context

	localAddrValue  net.Addr
	remoteAddrValue net.Addr
	tlsState        *tls.ConnectionState
	closeErr        error
}

func (m *fakeStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	return nil, nil
}

func (m *fakeStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	return nil, nil
}

func (m *fakeStreamConn) CloseWithError(code transport.ConnErrorCode, msg string) error {
	return m.closeErr
}

func (m *fakeStreamConn) Context() context.Context {
	if m.parentCtx != nil {
		return m.parentCtx
	}
	return context.Background()
}

func (m *fakeStreamConn) LocalAddr() net.Addr {
	if m.localAddrValue != nil {
		return m.localAddrValue
	}
	return &net.TCPAddr{}
}

func (m *fakeStreamConn) OpenStream() (transport.Stream, error) {
	return nil, nil
}

func (m *fakeStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	return nil, nil
}

func (m *fakeStreamConn) OpenUniStream() (transport.SendStream, error) {
	return nil, nil
}

func (m *fakeStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	return nil, nil
}

func (m *fakeStreamConn) RemoteAddr() net.Addr {
	if m.remoteAddrValue != nil {
		return m.remoteAddrValue
	}
	return &net.TCPAddr{}
}

func (m *fakeStreamConn) TLS() *tls.ConnectionState {
	return m.tlsState
}
