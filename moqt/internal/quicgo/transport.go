package quicgo

import (
	"net"

	"github.com/quic-go/quic-go"
)

// NewTransport returns a quic.Transport over a [Slot] holding conn, with
// zero-length connection IDs. A dialed connection uses them, and so must
// every path added to it: a path with quic-go's default length cannot carry
// it (quic-go#5185), and a server follows a client with non-zero-length
// ones only twice (quic-go#5965).
func NewTransport(conn net.PacketConn) (*quic.Transport, *Slot) {
	slot := newSlot(conn)
	return &quic.Transport{Conn: slot, ConnectionIDGenerator: zeroConnIDs{}}, slot
}

// zeroConnIDs generates zero-length connection IDs.
type zeroConnIDs struct{}

var _ quic.ConnectionIDGenerator = zeroConnIDs{}

func (zeroConnIDs) GenerateConnectionID() (quic.ConnectionID, error) {
	return quic.ConnectionID{}, nil
}

func (zeroConnIDs) ConnectionIDLen() int { return 0 }
