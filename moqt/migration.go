package moqt

import (
	"errors"

	"github.com/quic-go/quic-go"
)

// ErrMigrationUnsupported is returned by [Session.AddPath] for a session
// with no QUIC connection gomoqt can reach: a WebSocket one, a server's
// WebTransport one, or one over a connection from DialQUICFunc or
// DialWebTransportFunc.
var ErrMigrationUnsupported = errors.New("moqt: connection migration is not supported by this session")

// quicConn is implemented by a connection with a QUIC connection under it
// that gomoqt dialed.
type quicConn interface {
	QUICConn() *quic.Conn
}

// AddPath adds a network path over t to the session's QUIC connection, as
// [quic.Conn.AddPath] does: the connection, and the session on it, can then
// move onto t's socket (RFC 9000 §9) through the returned path's Probe and
// Switch, and stay the same. The application owns t and its socket, and
// chooses when to move; gomoqt applies no policy.
//
// It follows quic-go's model as it is:
//
//   - A transport is one socket: one per network, such as a socket bound to
//     cellular while the session runs over Wi-Fi. To move back onto the
//     socket the session was dialed from, dial with [Dialer.Transport] and
//     add that transport.
//   - Closing a transport the connection has used ends the connection
//     (quic-go#5975); close the path with quic.Path.Close instead, and the
//     transport once the session has ended.
//   - Every transport of the connection should use zero-length connection
//     IDs, as a Dialer with no [Dialer.Transport] does: give each one,
//     Dialer.Transport too, a quic.ConnectionIDGenerator whose
//     ConnectionIDLen is 0. A transport
//     with quic-go's default length cannot carry a path for such a
//     connection (quic-go#5185), and a quic-go server follows a client with
//     non-zero-length ones only twice (quic-go#5965).
//   - Every transport of the connection should be over the same kind of
//     socket, all *net.UDPConn or all not: quic-go keeps a connection's ECN
//     marking across a switch, and panics on a socket it cannot mark ECN on.
//
// A move from a network still working loses nothing. One from a network
// already gone takes until the server sees the client on the new path,
// which the client's next packet shows it: set the KeepAlivePeriod of
// [Dialer.QUICConfig] to bound that for a session that only receives.
//
// It returns [ErrMigrationUnsupported] for a session with no QUIC
// connection gomoqt can reach, a server's WebTransport session among them,
// and quic-go's error for one that cannot migrate: a server's native QUIC
// session, or one whose peer disabled migration.
func (s *Session) AddPath(t *quic.Transport) (*quic.Path, error) {
	qc, ok := s.conn.(quicConn)
	if !ok || qc.QUICConn() == nil {
		return nil, ErrMigrationUnsupported
	}
	return qc.QUICConn().AddPath(t)
}
