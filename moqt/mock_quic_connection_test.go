package moqt

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/stretchr/testify/mock"
)

var _ StreamConn = (*MockStreamConn)(nil)

// MockStreamConn is a mock implementation of StreamConn using testify/mock
type MockStreamConn struct {
	mock.Mock
	AcceptStreamFunc      func(ctx context.Context) (Stream, error)
	AcceptUniStreamFunc   func(ctx context.Context) (ReceiveStream, error)
	OpenStreamFunc        func() (Stream, error)
	OpenUniStreamFunc     func() (SendStream, error)
	OpenStreamSyncFunc    func(ctx context.Context) (Stream, error)
	OpenUniStreamSyncFunc func(ctx context.Context) (SendStream, error)
}

// TLS implements [StreamConn].
func (m *MockStreamConn) TLS() *tls.ConnectionState {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*tls.ConnectionState)
}

func (m *MockStreamConn) AcceptStream(ctx context.Context) (Stream, error) {
	if m.AcceptStreamFunc != nil {
		return m.AcceptStreamFunc(ctx)
	}
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(Stream), args.Error(1)
}

func (m *MockStreamConn) AcceptUniStream(ctx context.Context) (ReceiveStream, error) {
	if m.AcceptUniStreamFunc != nil {
		return m.AcceptUniStreamFunc(ctx)
	}
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ReceiveStream), args.Error(1)
}

func (m *MockStreamConn) OpenStream() (Stream, error) {
	if m.OpenStreamFunc != nil {
		return m.OpenStreamFunc()
	}
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(Stream), args.Error(1)
}

func (m *MockStreamConn) OpenUniStream() (SendStream, error) {
	if m.OpenUniStreamFunc != nil {
		return m.OpenUniStreamFunc()
	}
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(SendStream), args.Error(1)
}

func (m *MockStreamConn) OpenStreamSync(ctx context.Context) (Stream, error) {
	if m.OpenStreamSyncFunc != nil {
		return m.OpenStreamSyncFunc(ctx)
	}
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(Stream), args.Error(1)
}

func (m *MockStreamConn) OpenUniStreamSync(ctx context.Context) (SendStream, error) {
	if m.OpenUniStreamSyncFunc != nil {
		return m.OpenUniStreamSyncFunc(ctx)
	}
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(SendStream), args.Error(1)
}

func (m *MockStreamConn) LocalAddr() net.Addr {
	args := m.Called()
	return args.Get(0).(net.Addr)
}

func (m *MockStreamConn) RemoteAddr() net.Addr {
	args := m.Called()
	return args.Get(0).(net.Addr)
}

func (m *MockStreamConn) CloseWithError(code ConnErrorCode, reason string) error {
	args := m.Called(code, reason)
	return args.Error(0)
}

func (m *MockStreamConn) Context() context.Context {
	args := m.Called()
	return args.Get(0).(context.Context)
}
