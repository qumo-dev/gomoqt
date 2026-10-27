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
	// whose connection cannot move between networks: a server's, one whose
	// peer disabled migration, a WebSocket one, or one over a connection
	// from DialQUICFunc or DialWebTransportFunc.
	ErrMigrationUnsupported = errors.New("moqt: connection migration is not supported by this session")

	// ErrPathClosed is returned for a [Path] that has been closed.
	ErrPathClosed = errors.New("moqt: path closed")

	// ErrPathNotValidated is returned by [Path.Switch] for a path that
	// [Path.Probe] has not validated.
	ErrPathNotValidated = errors.New("moqt: path not validated")

	// ErrPathActive is returned by [Path.Close] for the path the connection
	// is on: switch to another path, or rebind this one, first.
	ErrPathActive = errors.New("moqt: path is active")
)

// Path is a network path a client session's QUIC connection can move onto
// (RFC 9000 §9): a socket bound to one network, such as cellular while the
// connection runs over Wi-Fi. The connection, and the session on it, stay
// the same across a move.
//
// The application decides which networks to use and when to move: it adds
// a path per network, probes and switches to the one it prefers, rebinds
// the active path to another socket, and closes paths it no longer needs.
// A move from a network still working loses nothing; one from a network
// already gone takes until the server sees the client on the new path,
// which the client's next packet shows it: set [quic.Config.KeepAlivePeriod]
// to bound that for a session that only receives.
//
// The session starts on the socket it was dialed from, which is bound to no
// network and follows the OS's default one.
type Path struct {
	set  *pathSet
	tr   *quic.Transport
	slot *quicgo.Slot

	// Guarded by set.mu.
	qp        *quic.Path // nil once closed or left
	validated bool       // qp has been validated
	closed    bool
}

// pathSet is a session's network paths. Closing a transport destroys the
// connection (quic-go#5975), so a closed path closes only its socket, and
// its transport, idle, is closed with the connection: each path added costs
// a transport until the session ends.
type pathSet struct {
	conn *quic.Conn

	mu     sync.Mutex
	closed bool  // the connection is gone
	active *Path // nil while on the dialed socket
	all    []*Path
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
	if err := ps.gone(); err != nil {
		_ = conn.Close() // not actionable: the connection is gone
		return nil, err
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

// gone is the reason the connection ended, or nil while it is up. ps.mu is
// held.
func (ps *pathSet) gone() error {
	if !ps.closed && ps.conn.Context().Err() == nil {
		return nil
	}
	if cause := context.Cause(ps.conn.Context()); cause != nil {
		return cause
	}
	return net.ErrClosed
}

// Probe validates the path: it checks that the server answers over it,
// without moving the connection. It returns once the server has answered,
// at once for a path already validated, or with ctx's error or the
// connection's.
func (p *Path) Probe(ctx context.Context) error {
	qp, err := p.startProbe()
	if err != nil || qp == nil {
		return err
	}
	// quic-go's probe does not end with the connection.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.set.conn.Context(), cancel)
	defer stop()
	if err := qp.Probe(ctx); err != nil {
		return p.probeFailed(qp, err)
	}
	p.probed(qp)
	return nil
}

// startProbe returns the quic-go path to probe, nil if p needs none.
func (p *Path) startProbe() (*quic.Path, error) {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if err := ps.gone(); err != nil {
		return nil, err
	}
	if p.closed {
		return nil, ErrPathClosed
	}
	if ps.active == p || p.validated {
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

// probed records that qp is validated, unless p has moved on from it.
func (p *Path) probed(qp *quic.Path) {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p.qp == qp {
		p.validated = true
	}
}

// probeFailed abandons qp after a failed probe, unless the connection moved
// onto it meanwhile, through an earlier probe, and returns the error to
// report for err.
func (p *Path) probeFailed(qp *quic.Path, err error) error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if goneErr := ps.gone(); goneErr != nil {
		return goneErr
	}
	if errors.Is(err, quic.ErrPathClosed) {
		return ErrPathClosed
	}
	if p.qp == qp && ps.active != p && !p.validated {
		p.qp = nil
		_ = qp.Close() // not actionable: the probe's error is the one to report
	}
	return err
}

// Switch moves the connection onto the path, which [Path.Probe] must have
// validated, and returns at once. The path left can be probed again to move
// back.
func (p *Path) Switch() error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p.closed {
		return ErrPathClosed
	}
	if ps.active == p {
		return nil
	}
	if p.qp == nil || !p.validated {
		return ErrPathNotValidated
	}
	if err := p.qp.Switch(); err != nil {
		if errors.Is(err, quic.ErrPathNotValidated) {
			return ErrPathNotValidated
		}
		return ErrPathClosed
	}
	// quic-go makes the new path active at once, so the one left can be
	// closed now, which returns its connection ID to the server for the
	// next move.
	if prev := ps.active; prev != nil {
		prev.abandon()
	}
	ps.active = p
	return nil
}

// Rebind replaces the path's socket with conn, which the path owns from
// here on, and closes the socket it replaces. On the active path the
// connection carries on from conn, and the server follows it as it follows
// any client whose address changes, without a probe: a quick way off a
// network that is gone, to a socket bound to no network or to another
// network. On a path not active, the next probe goes out from conn.
func (p *Path) Rebind(conn net.PacketConn) error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if err := ps.gone(); err != nil {
		_ = conn.Close() // not actionable: the connection is gone
		return err
	}
	if p.closed {
		_ = conn.Close() // not actionable: the path is closed
		return ErrPathClosed
	}
	if ps.active != p {
		// A validation holds for the socket it was made over.
		p.abandon()
	}
	p.slot.Set(conn)
	return nil
}

// Close closes the path and its socket. It returns [ErrPathActive] for the
// path the connection is on.
func (p *Path) Close() error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p.closed {
		return nil
	}
	if ps.active == p {
		return ErrPathActive
	}
	p.closed = true
	p.abandon()
	p.slot.Clear()
	return nil
}

// abandon closes p's quic-go path, which must not be active. set.mu is held.
func (p *Path) abandon() {
	if p.qp != nil {
		_ = p.qp.Close() // not actionable: a path not active closes without error
		p.qp = nil
	}
	p.validated = false
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
