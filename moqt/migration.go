package moqt

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/quicgo"
)

var (
	// ErrMigrationUnsupported is returned by [Session.AddPath] for a session
	// whose connection cannot move between networks: a server's, a WebSocket
	// one, or one over a connection from DialQUICFunc or
	// DialWebTransportFunc.
	ErrMigrationUnsupported = errors.New("moqt: connection migration is not supported by this session")

	// ErrPathClosed is returned for a [Path] that has been closed.
	ErrPathClosed = errors.New("moqt: path closed")

	// ErrPathNotValidated is returned by [Path.Switch] for a path that
	// [Path.Probe] has not validated.
	ErrPathNotValidated = errors.New("moqt: path not validated")
)

// Path is a network path a client session's QUIC connection can move onto
// (RFC 9000 §9): a socket bound to one network, such as cellular while the
// connection runs over Wi-Fi. The connection, and the session on it, stay
// the same across a move.
//
// The application learns which networks exist and creates the sockets; the
// session probes and switches paths when told. A move from a network still
// working loses nothing; one from a network already gone takes until the
// server sees the client on the new path, which the client's next packet
// shows it: set [quic.Config.KeepAlivePeriod] to bound that for a session
// that only receives.
//
// The session starts on the socket it was dialed from, which is bound to no
// network and follows the OS's default one. Closing the active path, when its
// network is gone, puts the connection back on such a socket at once, so the
// server follows it to whichever network the OS uses next, without waiting
// for a probe.
type Path struct {
	set  *pathSet
	tr   *quic.Transport
	slot *quicgo.Slot

	// Guarded by set.mu.
	qp     *quic.Path // nil once left or closed
	leftQP *quic.Path // the quic-go path left, until retired
	closed bool
}

// pathSet is a session's network paths. quic-go refuses to close the active
// path, and a switch takes effect only at the connection's next event, so a
// path the connection leaves is retired on the next call, by when the
// switch has taken effect; that returns its connection ID to the server for
// the next move. Closing a transport would destroy the connection
// (quic-go#5975), so a closed path closes only its socket, and its
// transport is closed with the connection.
type pathSet struct {
	conn *quic.Conn

	mu     sync.Mutex
	closed bool  // the connection is gone
	active *Path // nil while on the dialed socket
	left   []*Path
	all    []*Path // closed with the connection
}

// quicConn is implemented by a connection with a QUIC connection under it.
type quicConn interface {
	QUICConn() *quic.Conn
}

// AddPath adds a path over conn, a UDP socket bound to the network the path
// goes over, and takes ownership of conn: it is closed when the path is,
// when the session ends, or here, if the path cannot be added. Adding a path
// sends nothing; [Path.Probe] does.
//
// It returns [ErrMigrationUnsupported] for a session that cannot move.
func (s *Session) AddPath(conn net.PacketConn) (*Path, error) {
	ps := s.pathSet()
	if ps == nil {
		_ = conn.Close() // not actionable: the error says the path is refused
		return nil, ErrMigrationUnsupported
	}
	return ps.add(conn)
}

// pathSet is the session's paths, created on first use, or nil if its
// connection has no QUIC connection under it.
func (s *Session) pathSet() *pathSet {
	s.pathsMu.Lock()
	defer s.pathsMu.Unlock()
	if s.paths == nil {
		qc, ok := s.conn.(quicConn)
		if !ok || qc.QUICConn() == nil {
			return nil
		}
		ps := &pathSet{conn: qc.QUICConn()}
		context.AfterFunc(ps.conn.Context(), ps.close)
		s.paths = ps
	}
	return s.paths
}

func (ps *pathSet) add(conn net.PacketConn) (*Path, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.closed || ps.conn.Context().Err() != nil {
		_ = conn.Close() // not actionable: the connection is gone
		return nil, net.ErrClosed
	}
	tr, slot := quicgo.NewTransport(conn)
	// quic-go refuses a path on a connection that cannot migrate: a
	// server's, or one whose peer disabled migration.
	qp, err := ps.conn.AddPath(tr)
	if err != nil {
		_ = slot.Close() // not actionable: the path is refused
		return nil, errors.Join(ErrMigrationUnsupported, err)
	}
	p := &Path{set: ps, tr: tr, slot: slot, qp: qp}
	ps.all = append(ps.all, p)
	return p, nil
}

// Probe validates the path: it checks that the server answers over it,
// without moving the connection. It returns once the server has answered,
// or with ctx's error.
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
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.retireLeft()
	if p.closed {
		return nil, ErrPathClosed
	}
	if ps.active == p {
		return nil, nil
	}
	if p.qp == nil {
		qp, err := ps.conn.AddPath(p.tr)
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
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p.qp != qp || ps.active == p {
		return
	}
	p.qp = nil
	_ = qp.Close() // not actionable: the probe's error is the one to report
}

// Switch moves the connection onto the path, which [Path.Probe] must have
// validated, and returns at once. The path left can be probed again to move
// back.
func (p *Path) Switch() error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.retireLeft()
	if p.closed {
		return ErrPathClosed
	}
	if ps.active == p {
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
	if prev := ps.active; prev != nil {
		prev.leftQP, prev.qp = prev.qp, nil
		ps.left = append(ps.left, prev)
	}
	ps.active = p
	return nil
}

// Close closes the path and its socket. Closing the active path, whose
// network is gone, does not wait for another path to validate: the path
// moves onto a new socket bound to no network, which the OS routes over its
// default network, and the server follows the connection there as it
// follows any client whose address changes. That is far quicker than a
// probe over a network just brought up, whose first packets are often lost.
func (p *Path) Close() error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.retireLeft()
	if p.closed {
		return nil
	}
	if ps.active == p {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
		if err != nil {
			return err // the path stays open, on its own socket
		}
		p.closed = true
		p.slot.Set(udp)
		return nil
	}
	p.closed = true
	if p.qp != nil {
		_ = p.qp.Close() // not actionable: a path not active closes without error
		p.qp = nil
	}
	if p.leftQP == nil {
		p.slot.Clear() // else once the path is retired
	}
	return nil
}

// retireLeft closes the quic-go paths the connection has left, once quic-go
// lets it, and the socket of a left path that was closed. ps.mu is held.
func (ps *pathSet) retireLeft() {
	kept := ps.left[:0]
	for _, p := range ps.left {
		if p.leftQP.Close() != nil {
			kept = append(kept, p) // the switch has not taken effect yet
			continue
		}
		p.leftQP = nil
		if p.closed {
			p.slot.Clear()
		}
	}
	ps.left = kept
}

// close closes the socket and transport of every path added, once the
// connection has ended.
func (ps *pathSet) close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.closed = true
	for _, p := range ps.all {
		_ = p.slot.Close() // not actionable: the connection is gone
		_ = p.tr.Close()   // not actionable: as above
	}
	ps.all = nil
}
