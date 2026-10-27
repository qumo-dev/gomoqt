package moqt

import (
	"context"
	"errors"
	"net"

	"github.com/qumo-dev/gomoqt/moqt/internal/quicgo"
)

var (
	// ErrMigrationUnsupported is returned by [Session.AddPath] for a session
	// whose connection cannot move between networks: a server's, a WebSocket
	// one, or one over a connection from DialQUICFunc or
	// DialWebTransportFunc.
	ErrMigrationUnsupported = errors.New("moqt: connection migration is not supported by this session")

	// ErrPathClosed is returned for a [Path] that has been closed.
	ErrPathClosed = quicgo.ErrPathClosed

	// ErrPathNotValidated is returned by [Path.Switch] for a path that
	// [Path.Probe] has not validated.
	ErrPathNotValidated = quicgo.ErrPathNotValidated
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
	path *quicgo.Path
}

// migrator is implemented by a connection that can move between networks.
type migrator interface {
	Migrator() *quicgo.Migrator
}

// AddPath adds a path over conn, a UDP socket bound to the network the path
// goes over, and takes ownership of conn: it is closed when the path is,
// when the session ends, or here, if the path cannot be added. Adding a path
// sends nothing; [Path.Probe] does.
//
// It returns [ErrMigrationUnsupported] for a session that cannot move.
func (s *Session) AddPath(conn net.PacketConn) (*Path, error) {
	mc, ok := s.conn.(migrator)
	if !ok || mc.Migrator() == nil {
		_ = conn.Close() // not actionable: the error says the path is refused
		return nil, ErrMigrationUnsupported
	}
	p, err := mc.Migrator().AddPath(conn)
	if err != nil {
		return nil, err
	}
	return &Path{path: p}, nil
}

// Probe validates the path: it checks that the server answers over it,
// without moving the connection. It returns once the server has answered,
// or with ctx's error. A probe right after a switch first waits out a short
// spacing, a couple of round trips, that the connection needs between moves.
func (p *Path) Probe(ctx context.Context) error {
	return p.path.Probe(ctx)
}

// Switch moves the connection onto the path, which [Path.Probe] must have
// validated, and returns at once. The path left can be probed again to move
// back.
func (p *Path) Switch() error {
	return p.path.Switch()
}

// Close closes the path and its socket. Closing the active path moves the
// connection onto a new socket bound to no network, which the OS routes over
// its default network.
func (p *Path) Close() error {
	return p.path.Close()
}
