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
	m, err := Dial(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return &connWrapper{conn: m.conn, migrator: m}, nil
}

// Dial dials a 0-RTT QUIC connection to addr from a socket of its own, as
// [quic.DialAddrEarly] does, but one that keeps the connection through a
// network change: a packet the OS refuses because the network is going away
// (Wi-Fi off, moving to cellular) is treated as lost, so QUIC retransmits it
// once the OS routes over the next network, instead of closing the
// connection. A network that does not come back still ends the connection,
// by the idle timeout.
//
// The client uses zero-length connection IDs, as [quic.DialAddrEarly] does,
// so a server keeps following it however often its address changes. The
// returned [Migrator] moves the connection onto other paths on request.
func Dial(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*Migrator, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	// Infer the server name from the host dialed, as quic.DialAddrEarly does:
	// dialing a resolved address would otherwise take it from the IP.
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = serverName(addr)
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	// A transport of our own, unlike quic.DialEarly's, so that the
	// connection can move back onto it after moving to another path.
	//
	// Its socket is a slot, as every path's is, not the *net.UDPConn
	// itself: quic-go fixes a connection's ECN marking from the socket it
	// was dialed from and keeps it on a switch, so a connection dialed from
	// a socket quic-go can mark ECN on panics once it moves to one it
	// cannot. Every socket of the connection is therefore one quic-go sees
	// as plain, and the connection sends without ECN or GSO.
	home := newSocketSlot(udp)
	tr := &quic.Transport{Conn: home, ConnectionIDGenerator: zeroConnIDs{}}
	conn, err := tr.DialEarly(ctx, raddr, tlsConfig, quicConfig)
	if err != nil {
		_ = home.Close() // not actionable: the dial error is the one to report
		_ = tr.Close()   // not actionable: as above
		return nil, err
	}
	m := newMigrator(conn, tr)
	// The transport and socket are ours, not quic-go's: close them, and
	// every path added, with the connection. A slot closes before its
	// transport, so the transport's read ends.
	context.AfterFunc(conn.Context(), func() {
		m.close()
		_ = home.Close() // not actionable: the connection is already gone
		_ = tr.Close()   // not actionable: as above
	})
	return m, nil
}

// serverName is the host part of addr, or addr itself when it has no port.
func serverName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
