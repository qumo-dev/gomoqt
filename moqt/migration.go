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
	// is on: switch to another path first.
	ErrPathActive = errors.New("moqt: path is active")

	// ErrDefaultPath is returned by [Path.Close] for the default path, which
	// lives as long as the session.
	ErrDefaultPath = errors.New("moqt: the default path cannot be closed")
)

// Path is one socket a client session's QUIC connection can send and
// receive over (RFC 9000 §9), for the path's whole life: a socket bound to
// one network, such as cellular while the connection runs over Wi-Fi, or
// the session's default path, the socket it was dialed from. The
// connection, and the session on it, stay the same across a move.
//
// gomoqt chooses no migration policy and never changes a path's socket: the
// application adds a path per network, probes it, switches to the one it
// prefers and closes the ones it no longer needs. A move from a network
// still working loses nothing; one from a network already gone takes until
// the server sees the client on the new path, which the client's next
// packet shows it: set [quic.Config.KeepAlivePeriod] to bound that for a
// session that only receives.
type Path struct {
	set       *pathSet
	tr        *quic.Transport
	slot      *quicgo.Slot // nil for the default path, whose socket the dial owns
	isDefault bool

	// Guarded by set.mu.
	qp        *quic.Path // nil until probed, and once left or closed
	validated bool       // qp has been validated
	closed    bool
}

// pathSet is a session's paths. Closing a transport destroys the connection
// (quic-go#5975), so a closed path closes only its socket, and its
// transport, idle, is closed with the connection: each path added costs a
// transport until the session ends.
type pathSet struct {
	conn *quic.Conn
	def  *Path // the default path

	mu     sync.Mutex
	closed bool // the connection is gone
	active *Path
	added  []*Path
}

// quicConn is implemented by a connection dialed from a quic.Transport.
type quicConn interface {
	QUICConn() *quic.Conn
	QUICTransport() *quic.Transport
}

// DefaultPath is the path over the socket the session was dialed from,
// which is bound to no network, so the OS sends it over whichever network
// is its default: the connection starts on it, and moves with the OS's
// default network while on it, without the application doing anything.
// It is nil for a session that cannot move (see [Session.AddPath]).
//
// Switching back to it, as when the active path's network is gone, needs a
// probe, unless it was probed after the connection left it. Keeping it
// ready that way costs one probe after each move off it, and, while it is
// validated but not active, holds one of the few connection IDs the server
// grants the client (quic-go grants three beyond the one in use), which
// other paths then cannot probe with.
func (s *Session) DefaultPath() *Path {
	ps := s.pathSet()
	if ps == nil {
		return nil
	}
	return ps.def
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
// connection was not dialed from a quic.Transport by gomoqt.
func (s *Session) pathSet() *pathSet {
	s.pathsMu.Lock()
	defer s.pathsMu.Unlock()
	if s.paths == nil {
		qc, ok := s.conn.(quicConn)
		if !ok || qc.QUICConn() == nil || qc.QUICTransport() == nil {
			return nil
		}
		s.paths = newPathSet(qc.QUICConn(), qc.QUICTransport())
	}
	return s.paths
}

// newPathSet returns the paths of conn, dialed from tr, which is the default
// path's transport and the one the connection starts on.
func newPathSet(conn *quic.Conn, tr *quic.Transport) *pathSet {
	ps := &pathSet{conn: conn}
	ps.def = &Path{set: ps, tr: tr, isDefault: true}
	ps.active = ps.def
	context.AfterFunc(conn.Context(), ps.close)
	return ps
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
	ps.added = append(ps.added, p)
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
// at once for the active path or one already validated, or with ctx's
// error or the connection's.
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
	ps.active.abandon()
	ps.active = p
	return nil
}

// Close closes the path and its socket. It returns [ErrPathActive] for the
// path the connection is on, and [ErrDefaultPath] for the default path.
func (p *Path) Close() error {
	ps := p.set
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p.isDefault {
		return ErrDefaultPath
	}
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
// connection has ended. The default path's are its dial's to close.
func (ps *pathSet) close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.closed = true
	for _, p := range ps.added {
		_ = p.slot.Close() // not actionable: the connection is gone
		_ = p.tr.Close()   // not actionable: as above
	}
	ps.added = nil
}
