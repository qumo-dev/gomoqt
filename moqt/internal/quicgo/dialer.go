package quicgo

import (
	"context"
	"crypto/tls"
	"net"

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
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	// Infer the server name from the host dialed, as quic.DialAddrEarly does:
	// quic.DialEarly would otherwise take it from the resolved IP address.
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = serverName(addr)
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	conn, err := quic.DialEarly(ctx, netChangeTolerantConn{udp}, raddr, tlsConfig, quicConfig)
	if err != nil {
		_ = udp.Close() // not actionable: the dial error is the one to report
		return nil, err
	}
	// quic.DialEarly does not own a socket it is given: close it with the
	// connection.
	context.AfterFunc(conn.Context(), func() {
		_ = udp.Close() // not actionable: the connection is already gone
	})
	return conn, nil
}

// DialTransportEarly dials a native QUIC connection to addr over tr, as
// [quic.Transport.DialEarly] does, taking the server name from the host
// dialed when tlsConfig names none. tr, and the socket under it, are the
// caller's: the connection ends if tr is closed.
func DialTransportEarly(ctx context.Context, tr *quic.Transport, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.StreamConn, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = serverName(addr)
	}
	conn, err := tr.DialEarly(ctx, raddr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return wrapConnection(conn), nil
}

// serverName is the host part of addr, or addr itself when it has no port.
func serverName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
