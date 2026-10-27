package quicgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// DialAddrEarly dials a native QUIC connection to addr through [Dial].
func DialAddrEarly(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.StreamConn, error) {
	conn, tr, err := Dial(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return &connWrapper{conn: conn, tr: tr}, nil
}

// Dial dials a 0-RTT QUIC connection to addr from a socket of its own, as
// [quic.DialAddrEarly] does, but one that keeps the connection through a
// network change: a packet the OS refuses because the network is going away
// (Wi-Fi off, moving to cellular) is treated as lost, so QUIC retransmits it
// once the OS routes over the next network, instead of closing the
// connection. A network that does not come back still ends the connection,
// by the idle timeout.
//
// The transport, returned with the connection, is one from [NewTransport],
// so the client uses zero-length connection IDs, a server keeps following
// it however often its address changes, and paths over it, or over other
// transports from NewTransport, can be added to the connection.
func Dial(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, *quic.Transport, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, nil, err
	}
	// Infer the server name from the host dialed, as quic.DialAddrEarly does:
	// dialing a resolved address would otherwise take it from the IP.
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = serverName(addr)
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, err
	}
	// The socket goes in a slot, as every added path's does, not to quic-go
	// itself: quic-go fixes a connection's ECN marking from the socket it was
	// dialed from and keeps it on a switch, so a connection dialed from a
	// socket quic-go can mark ECN on panics once it moves to one it cannot.
	// Every socket of the connection is therefore one quic-go sees as plain,
	// and the connection sends without ECN or GSO.
	tr, slot := NewTransport(udp)
	conn, err := tr.DialEarly(ctx, raddr, tlsConfig, quicConfig)
	if err != nil {
		_ = slot.Close() // not actionable: the dial error is the one to report
		_ = tr.Close()   // not actionable: as above
		return nil, nil, err
	}
	// The transport and socket are ours, not quic-go's: close them with the
	// connection, the slot first, so the transport's read ends.
	context.AfterFunc(conn.Context(), func() {
		_ = slot.Close() // not actionable: the connection is already gone
		_ = tr.Close()   // not actionable: as above
	})
	return conn, tr, nil
}

// serverName is the host part of addr, or addr itself when it has no port.
func serverName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
