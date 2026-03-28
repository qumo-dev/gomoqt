package webtransportgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/okdaichi/gomoqt/transport"
	"github.com/stretchr/testify/mock"
)

var _ transport.StreamConn = (*MockStreamConn)(nil)

type MockStreamConn struct {
	mock.Mock
}

func (m *MockStreamConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	args := m.Called(ctx)
	stream, _ := args.Get(0).(transport.Stream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) AcceptUniStream(ctx context.Context) (transport.ReceiveStream, error) {
	args := m.Called(ctx)
	stream, _ := args.Get(0).(transport.ReceiveStream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) CloseWithError(code transport.ConnErrorCode, msg string) error {
	args := m.Called(code, msg)
	return args.Error(0)
}

func (m *MockStreamConn) Context() context.Context {
	args := m.Called()
	ctx, _ := args.Get(0).(context.Context)
	return ctx
}

func (m *MockStreamConn) LocalAddr() net.Addr {
	args := m.Called()
	addr, _ := args.Get(0).(net.Addr)
	return addr
}

func (m *MockStreamConn) OpenStream() (transport.Stream, error) {
	args := m.Called()
	stream, _ := args.Get(0).(transport.Stream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) OpenStreamSync(ctx context.Context) (transport.Stream, error) {
	args := m.Called(ctx)
	stream, _ := args.Get(0).(transport.Stream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) OpenUniStream() (transport.SendStream, error) {
	args := m.Called()
	stream, _ := args.Get(0).(transport.SendStream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) OpenUniStreamSync(ctx context.Context) (transport.SendStream, error) {
	args := m.Called(ctx)
	stream, _ := args.Get(0).(transport.SendStream)
	return stream, args.Error(1)
}

func (m *MockStreamConn) RemoteAddr() net.Addr {
	args := m.Called()
	addr, _ := args.Get(0).(net.Addr)
	return addr
}

func (m *MockStreamConn) TLS() *tls.ConnectionState {
	args := m.Called()
	state, _ := args.Get(0).(*tls.ConnectionState)
	return state
}
