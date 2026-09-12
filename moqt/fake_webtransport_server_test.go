package moqt

import "sync"

var _ WebTransportServer = (*FakeWebTransportServer)(nil)

// FakeWebTransportServer is a fake implementation of the WebTransportServer interface.
type FakeWebTransportServer struct {
	mu sync.Mutex

	// Error overrides; the zero value means the call succeeds.
	ServeQUICConnErr error
	CloseErr         error

	// ServeNotify receives each served connection. Sends are non-blocking.
	ServeNotify chan<- StreamConn

	served []StreamConn
}

func (m *FakeWebTransportServer) ServeQUICConn(conn StreamConn) error {
	m.mu.Lock()
	m.served = append(m.served, conn)
	err := m.ServeQUICConnErr
	notify := m.ServeNotify
	m.mu.Unlock()

	if notify != nil {
		select {
		case notify <- conn:
		default:
		}
	}
	return err
}

// Served returns the connections passed to ServeQUICConn, in order.
func (m *FakeWebTransportServer) Served() []StreamConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]StreamConn, len(m.served))
	copy(out, m.served)
	return out
}

func (m *FakeWebTransportServer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CloseErr
}
