package moqt

import "net"

// TransportAddr is an optional interface that may be implemented by the
// net.Addr values returned from [Session.LocalAddr] and [Session.RemoteAddr].
// Callers can use a type assertion to retrieve the transport-layer information:
//
//	if ta, ok := sess.RemoteAddr().(moqt.TransportAddr); ok {
//	    fmt.Println(ta.Transport()) // "quic" or "webtransport"
//	}
//
// The design mirrors [http.Flusher]: the base API (net.Addr) stays simple,
// and extra capabilities are opt-in via interface assertion.
type TransportAddr interface {
	// Transport returns the transport protocol used for this connection.
	// Known values are "quic" and "webtransport".
	Transport() string
}

// addr wraps a net.Addr and annotates it with the transport protocol.
// It implements both net.Addr and TransportAddr.
type addr struct {
	addr      net.Addr
	transport string
}

var _ net.Addr = (*addr)(nil)
var _ TransportAddr = (*addr)(nil)

func (a *addr) Network() string   { return a.addr.Network() }
func (a *addr) String() string    { return a.addr.String() }
func (a *addr) Transport() string { return a.transport }
