package quicgo

import (
	"errors"
	"net"
	"sync/atomic"
)

// netChangeTolerantConn is a client UDP socket that, once its connection is
// established, reports a write the OS refuses because the network is
// changing as sent, so QUIC sees the packet as lost and retransmits it
// rather than closing the connection: quic-go closes a connection on any
// write error but "message too large". Until then such a refusal fails the
// dial at once, as it would without the wrapper.
//
// A peer that stays unreachable, or a network that does not come back, then
// ends the connection only by its idle timeout.
//
// It embeds *net.UDPConn so quic-go still finds the socket's ECN, packet-info
// and GSO support, and it covers both of quic-go's write paths: WriteMsgUDP
// for such a socket, WriteTo otherwise.
type netChangeTolerantConn struct {
	*net.UDPConn

	// established is set once the connection's handshake completes.
	established *atomic.Bool
}

// WriteTo writes b to addr, reporting a send refused for a network change as
// sent once the connection is established.
func (c netChangeTolerantConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := c.UDPConn.WriteTo(b, addr)
	if c.lost(err) {
		return len(b), nil
	}
	return n, err
}

// WriteMsgUDP writes b with its control data oob to addr, reporting a send
// refused for a network change as sent once the connection is established.
func (c netChangeTolerantConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.UDPConn.WriteMsgUDP(b, oob, addr)
	if c.lost(err) {
		return len(b), len(oob), nil
	}
	return n, oobn, err
}

// lost reports whether a send that failed with err is to count as a lost
// packet: one the OS refused for a network change, once established.
func (c netChangeTolerantConn) lost(err error) bool {
	return err != nil && c.established.Load() && isNetChangeErr(err)
}

// isNetChangeErr reports whether a send failed because the OS has no route
// to the peer for now, as while a phone moves between Wi-Fi and cellular:
// with one of netChangeErrnos, which each OS lists.
func isNetChangeErr(err error) bool {
	for _, errno := range netChangeErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
