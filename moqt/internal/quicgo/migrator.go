package quicgo

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

var (
	// ErrPathClosed is returned for a path that has been closed.
	ErrPathClosed = errors.New("moqt: path closed")
	// ErrPathNotValidated is returned by [Path.Switch] for a path not yet
	// validated by [Path.Probe].
	ErrPathNotValidated = errors.New("moqt: path not validated")
)

// Migrator moves a dialed client connection between network paths (RFC 9000
// §9) through quic-go's [quic.Conn.AddPath], around quic-go's limits:
//
//   - each path's transport has zero-length connection IDs, as the
//     connection was dialed with: quic-go's default length cannot carry a
//     path for it (quic-go#5185), and a server follows a client with
//     non-zero-length ones only twice (quic-go#5965);
//   - closing a transport destroys the connection (quic-go#5975), so a path
//     closes its socket, under the transport, never the transport, until
//     the connection ends;
//   - quic-go refuses to close the active path, and a switch takes effect
//     only at the connection's next event, so a path left is retired on the
//     next call, by when the switch has taken effect, which returns its
//     connection ID to the server for the next move.
//
// The connection starts on the socket it was dialed from, bound to no
// network, so the OS routes it over its default network.
type Migrator struct {
	conn *quic.Conn

	mu     sync.Mutex
	closed bool  // the connection is gone
	active *Path // nil while on the dialed socket
	left   []*Path
	paths  []*Path // every path added, closed with the connection
}

// Path is a network path a [Migrator] can move its connection onto.
type Path struct {
	m    *Migrator
	tr   *quic.Transport
	slot *socketSlot

	// Guarded by m.mu.
	qp     *quic.Path // nil until probed, and once left or closed
	leftQP *quic.Path // the quic-go path left, until retired
	closed bool
}

func newMigrator(conn *quic.Conn) *Migrator {
	return &Migrator{conn: conn}
}

// Conn is the connection the migrator moves.
func (m *Migrator) Conn() *quic.Conn { return m.conn }

// AddPath adds a path over conn, a socket bound to the network the path
// goes over, which the path owns from here on: it is closed with the path,
// with the connection, or here, if the path cannot be added.
func (m *Migrator) AddPath(conn net.PacketConn) (*Path, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.conn.Context().Err() != nil {
		_ = conn.Close() // not actionable: the connection is gone
		return nil, net.ErrClosed
	}
	slot := newSocketSlot(conn)
	p := &Path{m: m, tr: &quic.Transport{Conn: slot, ConnectionIDGenerator: zeroConnIDs{}}, slot: slot}
	m.paths = append(m.paths, p)
	return p, nil
}

// Probe validates the path: it returns once the server has answered over
// it, or with ctx's error.
func (p *Path) Probe(ctx context.Context) error {
	qp, err := p.startProbe()
	if err != nil || qp == nil {
		return err
	}
	if err := qp.Probe(ctx); err != nil {
		p.probeFailed(qp)
		if errors.Is(err, quic.ErrPathClosed) {
			return ErrPathClosed
		}
		return err
	}
	return nil
}

// startProbe returns the quic-go path to probe, nil if p is active.
func (p *Path) startProbe() (*quic.Path, error) {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retireLeft()
	if p.closed {
		return nil, ErrPathClosed
	}
	if m.active == p {
		return nil, nil
	}
	if p.qp == nil {
		qp, err := m.conn.AddPath(p.tr)
		if err != nil {
			return nil, err
		}
		p.qp = qp
	}
	return p.qp, nil
}

// probeFailed abandons qp after a failed probe, unless the connection has
// moved onto it meanwhile, through an earlier probe.
func (p *Path) probeFailed(qp *quic.Path) {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.qp != qp || m.active == p {
		return
	}
	p.qp = nil
	_ = qp.Close() // not actionable: the probe's error is the one to report
}

// Switch moves the connection onto the path, validated by Probe.
func (p *Path) Switch() error {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retireLeft()
	if p.closed {
		return ErrPathClosed
	}
	if m.active == p {
		return nil
	}
	if p.qp == nil {
		return ErrPathNotValidated
	}
	if err := p.qp.Switch(); err != nil {
		if errors.Is(err, quic.ErrPathNotValidated) {
			return ErrPathNotValidated
		}
		return ErrPathClosed
	}
	if prev := m.active; prev != nil {
		prev.leftQP, prev.qp = prev.qp, nil
		m.left = append(m.left, prev)
	}
	m.active = p
	return nil
}

// Close gives up the path and its socket. Closing the active path, whose
// network is gone, does not wait for another path to validate: the path
// moves onto a new socket bound to no network, which the OS routes over its
// default network, and the server follows the connection there as it
// follows any client whose address changes. That is far quicker than a
// probe over a network just brought up, whose first packets are often lost.
func (p *Path) Close() error {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retireLeft()
	if p.closed {
		return nil
	}
	if m.active == p {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
		if err != nil {
			return err // the path stays open, on its own socket
		}
		p.closed = true
		p.slot.set(udp)
		return nil
	}
	p.closed = true
	if p.qp != nil {
		_ = p.qp.Close() // not actionable: a path not active closes without error
		p.qp = nil
	}
	if p.leftQP == nil {
		p.slot.clear() // else once the path is retired
	}
	return nil
}

// retireLeft closes the quic-go paths the connection has left, once quic-go
// lets it, and the socket of a left path that was closed. m.mu is held.
func (m *Migrator) retireLeft() {
	kept := m.left[:0]
	for _, p := range m.left {
		if p.leftQP.Close() != nil {
			kept = append(kept, p) // the switch has not taken effect yet
			continue
		}
		p.leftQP = nil
		if p.closed {
			p.slot.clear()
		}
	}
	m.left = kept
}

// close closes the socket and transport of every path added, once the
// connection has ended.
func (m *Migrator) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, p := range m.paths {
		_ = p.slot.Close() // not actionable: the connection is gone
		_ = p.tr.Close()   // not actionable: as above
	}
	m.paths = nil
}

// zeroConnIDs generates zero-length connection IDs.
type zeroConnIDs struct{}

var _ quic.ConnectionIDGenerator = zeroConnIDs{}

func (zeroConnIDs) GenerateConnectionID() (quic.ConnectionID, error) {
	return quic.ConnectionID{}, nil
}

func (zeroConnIDs) ConnectionIDLen() int { return 0 }
