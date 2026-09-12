package webtransportgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.StreamConn = (*FakeStreamConn)(nil)

// FakeStreamConn is a zero-configuration stub connection: every stream
// operation reports no stream and no error. Set ParentCtx to control the
// connection context; set the value fields to override an address or the TLS
// state.
type FakeStreamConn struct {
	ParentCtx context.Context

	LocalAddrValue  net.Addr
	RemoteAddrValue net.Addr
	TLSState        *tls.ConnectionState
	CloseErr        error
}

func (m *FakeStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	return nil, nil
}

func (m *FakeStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	return nil, nil
}

func (m *FakeStreamConn) CloseWithError(code transport.ConnErrorCode, msg string) error {
	return m.CloseErr
}

func (m *FakeStreamConn) Context() context.Context {
	if m.ParentCtx != nil {
		return m.ParentCtx
	}
	return context.Background()
}

func (m *FakeStreamConn) LocalAddr() net.Addr {
	if m.LocalAddrValue != nil {
		return m.LocalAddrValue
	}
	return &net.TCPAddr{}
}

func (m *FakeStreamConn) OpenStream() (transport.Stream, error) {
	return nil, nil
}

func (m *FakeStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	return nil, nil
}

func (m *FakeStreamConn) OpenUniStream() (transport.SendStream, error) {
	return nil, nil
}

func (m *FakeStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	return nil, nil
}

func (m *FakeStreamConn) RemoteAddr() net.Addr {
	if m.RemoteAddrValue != nil {
		return m.RemoteAddrValue
	}
	return &net.TCPAddr{}
}

func (m *FakeStreamConn) TLS() *tls.ConnectionState {
	return m.TLSState
}
