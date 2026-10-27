package moqt

import (
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// low-level utility: the network change under test happens below any
// gomoqt interface, at the addresses a server sees, so the fake is a UDP
// relay.

// fakeNAT relays UDP between client sockets and a target, giving each
// client socket its own upstream socket, as a NAT gives each flow its own
// public address. remap gives one client socket a new upstream socket, as
// when the OS sends a socket bound to no network over another network: the
// target sees that client at a new address, and replies to the old one are
// lost.
type fakeNAT struct {
	front  *net.UDPConn
	target *net.UDPAddr

	mu    sync.Mutex
	flows map[string]*net.UDPConn // upstream socket by client address
}

func startFakeNAT(tb testing.TB, target string) *fakeNAT {
	tb.Helper()
	ta, err := net.ResolveUDPAddr("udp", target)
	require.NoError(tb, err)
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	n := &fakeNAT{front: front, target: ta, flows: map[string]*net.UDPConn{}}
	tb.Cleanup(func() {
		_ = front.Close() // not actionable: cleanup
		n.mu.Lock()
		defer n.mu.Unlock()
		for _, up := range n.flows {
			_ = up.Close() // not actionable: cleanup
		}
	})
	// The relay ends when the cleanup closes its sockets.
	go n.fromClients()
	return n
}

// addr is the address clients send to.
func (n *fakeNAT) addr() string { return n.front.LocalAddr().String() }

// remap gives the client socket at client a new upstream socket, closing
// the old one, and returns the new public address the target sees.
func (n *fakeNAT) remap(tb testing.TB, client net.Addr) net.Addr {
	tb.Helper()
	up, err := listenUp()
	require.NoError(tb, err)
	n.mu.Lock()
	old := n.flows[client.String()]
	n.flows[client.String()] = up
	n.mu.Unlock()
	if old != nil {
		_ = old.Close() // not actionable: the old address is meant to go away
	}
	ca := client.(*net.UDPAddr)
	go n.toClient(up, ca)
	return up.LocalAddr()
}

// listenUp opens an upstream socket.
func listenUp() (*net.UDPConn, error) {
	return net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
}

func (n *fakeNAT) fromClients() {
	buf := make([]byte, 2048)
	for {
		c, client, err := n.front.ReadFromUDP(buf)
		if err != nil {
			return // the relay is closed
		}
		up, err := n.flow(client)
		if err != nil {
			continue // drop: QUIC retransmits, and the next datagram tries again
		}
		_, _ = up.WriteToUDP(buf[:c], n.target) // not actionable: a lost datagram is QUIC's to recover
	}
}

// flow is the upstream socket for client, opened on its first datagram.
// It looks up and opens under one lock, so that a concurrent remap is not
// overwritten.
func (n *fakeNAT) flow(client *net.UDPAddr) (*net.UDPConn, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if up := n.flows[client.String()]; up != nil {
		return up, nil
	}
	up, err := listenUp()
	if err != nil {
		return nil, err
	}
	n.flows[client.String()] = up
	go n.toClient(up, client)
	return up, nil
}

func (n *fakeNAT) toClient(up *net.UDPConn, client *net.UDPAddr) {
	buf := make([]byte, 2048)
	for {
		c, _, err := up.ReadFromUDP(buf)
		if err != nil {
			return // this flow was remapped or the relay is closed
		}
		_, _ = n.front.WriteToUDP(buf[:c], client) // not actionable: as above
	}
}
