package quicgo

import (
	"context"
	"crypto/tls"
	"net"
	"sync/atomic"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// DialAddrEarly dials a native QUIC connection to addr through [DialEarly].
func DialAddrEarly(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.StreamConn, error) {
	conn, err := DialEarly(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return wrapConnection(conn), nil
}

// DialEarly dials a 0-RTT QUIC connection to addr from a socket of its own,
// as [quic.DialAddrEarly] does, but one that keeps the connection through a
// network change: a packet the OS refuses because the network is going away
// (Wi-Fi off, moving to cellular) is treated as lost, so QUIC retransmits it
// once the OS routes over the next network, instead of closing the
// connection. A network that does not come back still ends the connection,
// by the idle timeout.
//
// The client uses zero-length connection IDs, as [quic.DialAddrEarly] does,
// so a server keeps following it however often its address changes.
func DialEarly(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
	raddr, tlsConfig, err := resolve(addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	// Bind as quic.DialAddrEarly does: Go binds a dual-stack socket where
	// the OS has one.
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	var established atomic.Bool
	conn, err := quic.DialEarly(ctx, netChangeTolerantConn{UDPConn: udp, established: &established}, raddr, tlsConfig, quicConfig)
	if err != nil {
		_ = udp.Close() // not actionable: the dial error is the one to report
		return nil, err
	}
	// quic.DialEarly does not own a socket it is given: close it with the
	// connection.
	context.AfterFunc(conn.Context(), func() {
		_ = udp.Close() // not actionable: the connection is already gone
	})
	// Tolerate a network change once the handshake is done: before, a send
	// the OS refuses fails the dial at once, with its error.
	go func() {
		select {
		case <-conn.HandshakeComplete():
			established.Store(true)
		case <-conn.Context().Done():
		}
	}()
	return conn, nil
}

// resolve resolves addr as quic.DialAddrEarly does, and returns tlsConfig
// with the server name taken from the host dialed when it names none:
// quic.DialEarly would otherwise take it from the resolved IP address.
// tlsConfig itself is not changed.
func resolve(addr string, tlsConfig *tls.Config) (*net.UDPAddr, *tls.Config, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, nil, err
	}
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = serverName(addr)
	}
	return raddr, tlsConfig, nil
}

// serverName is the host part of addr, or addr itself when it has no port.
func serverName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// DialTransportEarly dials a native QUIC connection to addr through
// [DialTransportConnEarly].
func DialTransportEarly(ctx context.Context, tr *quic.Transport, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.StreamConn, error) {
	conn, err := DialTransportConnEarly(ctx, tr, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return wrapConnection(conn), nil
}

// DialTransportConnEarly dials a 0-RTT QUIC connection to addr over tr, as
// [quic.Transport.DialEarly] does, taking the server name from the host
// dialed when tlsConfig names none. tr, and the socket under it, are the
// caller's, used as they are: the connection ends if tr is closed, or if the
// OS refuses a send on its socket.
func DialTransportConnEarly(ctx context.Context, tr *quic.Transport, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
	raddr, tlsConfig, err := resolve(addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	return tr.DialEarly(ctx, raddr, tlsConfig, quicConfig)
}
