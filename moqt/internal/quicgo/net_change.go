package quicgo

import "net"

// netChangeTolerantConn is a client UDP socket that reports a write the OS
// refuses because the network is changing as sent, so QUIC sees the packet as
// lost and retransmits it rather than closing the connection: quic-go closes a
// connection on any write error but "message too large".
//
// It embeds *net.UDPConn so quic-go still finds the socket's ECN, packet-info
// and GSO support, and it covers both of quic-go's write paths: WriteMsgUDP
// for such a socket, WriteTo otherwise.
type netChangeTolerantConn struct {
	*net.UDPConn
}

func (c netChangeTolerantConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := c.UDPConn.WriteTo(b, addr)
	if err != nil && isNetChangeErr(err) {
		return len(b), nil
	}
	return n, err
}

func (c netChangeTolerantConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.UDPConn.WriteMsgUDP(b, oob, addr)
	if err != nil && isNetChangeErr(err) {
		return len(b), len(oob), nil
	}
	return n, oobn, err
}
