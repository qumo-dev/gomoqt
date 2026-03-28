package moqt

import (
	"context"
	"net"

	"github.com/stretchr/testify/mock"
)

var _ QUICListener = (*MockEarlyListener)(nil)

// MockEarlyListener implements a mock for quic.EarlyListener using mock.Mock
type MockEarlyListener struct {
	mock.Mock
}

// Accept mocks the Accept method of EarlyListener
func (m *MockEarlyListener) Accept(ctx context.Context) (StreamConn, error) {
	// New mock implementation
	args := m.Called(ctx)
	conn, _ := args.Get(0).(StreamConn)
	return conn, args.Error(1)
}

// Addr mocks the Addr method of EarlyListener
func (m *MockEarlyListener) Addr() net.Addr {
	// Check if using the mock
	if len(m.ExpectedCalls) > 0 {
		args := m.Called()
		if addr, ok := args.Get(0).(net.Addr); ok {
			return addr
		}
	}

	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

// Close mocks the Close method of EarlyListener
func (m *MockEarlyListener) Close() error {
	// Check if using the mock
	if len(m.ExpectedCalls) > 0 {
		args := m.Called()
		return args.Error(0)
	}

	return nil
}
