package quicgo

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
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
	host, raddr, err := resolveUDPAddr(ctx, addr)
	if err != nil {
		return nil, err
	}
	// Take the server name from the host dialed, as quic.DialAddrEarly does:
	// quic.DialEarly would otherwise take it from the resolved IP address.
	if tlsConfig != nil && tlsConfig.ServerName == "" {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = host
	}
	// Bind the family of the address dialed: a socket bound to IPv4 reaches
	// an IPv6 address only where the OS maps one into the other.
	local := &net.UDPAddr{IP: net.IPv4zero}
	if raddr.IP.To4() == nil {
		local.IP = net.IPv6unspecified
	}
	udp, err := net.ListenUDP("udp", local)
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

// resolveUDPAddr resolves addr, a host and port, within ctx, preferring an
// IPv4 address as net.ResolveUDPAddr does, and returns its host too.
func resolveUDPAddr(ctx context.Context, addr string) (string, *net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", nil, err
	}
	portNum, err := net.DefaultResolver.LookupPort(ctx, "udp", port)
	if err != nil {
		return "", nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", nil, err
	}
	ip := ips[0]
	for _, candidate := range ips {
		if candidate.Unmap().Is4() {
			ip = candidate
			break
		}
	}
	return host, net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip.Unmap(), uint16(portNum))), nil
}
