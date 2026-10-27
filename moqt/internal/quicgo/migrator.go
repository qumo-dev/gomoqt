package quicgo

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

var (
	// ErrPathClosed is returned for a path that has been closed.
	ErrPathClosed = errors.New("path closed")
	// ErrPathNotValidated is returned by [Path.Switch] for a path not yet
	// validated by [Path.Probe].
	ErrPathNotValidated = errors.New("path not validated")
)

// minSwitchSpacing is the least time between a switch and the next probe.
// After a switch the client retires the connection ID of the path it left,
// and the server issues the next one in return; probing again before that
// round trip completes can find no connection ID left to probe with, and,
// if the path the retirement went out on dies, none ever comes.
const minSwitchSpacing = 50 * time.Millisecond

// Migrator moves a dialed client connection between network paths (RFC 9000
// §9), as quic-go's [quic.Conn.AddPath] does, working around quic-go's
// limits in doing so:
//
//   - every transport it adds has zero-length connection IDs, as the
//     connection was dialed with: one with quic-go's default length cannot
//     carry a path for it (quic-go#5185), and a server follows a client with
//     non-zero-length ones only twice (quic-go#5965);
//   - a transport, once used, lives as long as the connection, since closing
//     it destroys the connection (quic-go#5975): a closed path gives up only
//     its socket, and its transport carries the next path added;
//   - a switch is applied at once and the path left is retired, which quic-go
//     leaves to the connection's next event and to the application;
//   - probes are spaced after a switch (see minSwitchSpacing).
//
// The connection starts on its home path: the socket it was dialed from,
// bound to no network, so the OS routes it over whichever network is the
// default. Closing the active path puts it back on such a socket.
type Migrator struct {
	conn *quic.Conn
	home *Path

	mu         sync.Mutex
	active     *Path
	lastSwitch time.Time
	spare      []*Path // closed paths whose transports wait for the next path
	added      []*Path // every path added, to close with the connection
}

// Path is a network path a [Migrator] can move its connection onto.
type Path struct {
	m    *Migrator
	tr   *quic.Transport
	slot *socketSlot // nil for the home path, whose socket is the dialer's

	// Guarded by m.mu.
	qp        *quic.Path // the quic-go path, nil until probed and once left
	validated bool
	closed    bool
}

func newMigrator(conn *quic.Conn, homeTr *quic.Transport) *Migrator {
	m := &Migrator{conn: conn}
	m.home = &Path{m: m, tr: homeTr}
	m.active = m.home
	return m
}

// Conn is the connection the migrator moves.
func (m *Migrator) Conn() *quic.Conn { return m.conn }

// AddPath adds a path over conn, a socket bound to the network the path
// goes over, which the path owns from here on: it is closed with the path,
// with the connection, or here, if the path cannot be added.
func (m *Migrator) AddPath(conn net.PacketConn) (*Path, error) {
	if err := context.Cause(m.conn.Context()); err != nil {
		_ = conn.Close() // not actionable: the connection is gone
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.spare); n > 0 {
		p := m.spare[n-1]
		m.spare = m.spare[:n-1]
		p.slot.set(conn)
		return &Path{m: m, tr: p.tr, slot: p.slot}, nil
	}
	slot := newSocketSlot(conn)
	p := &Path{m: m, tr: &quic.Transport{Conn: slot, ConnectionIDGenerator: zeroConnIDs{}}, slot: slot}
	m.added = append(m.added, p)
	return p, nil
}

// Probe validates the path, after waiting out the spacing that follows a
// switch. It returns once the server has answered, or with ctx's error.
func (p *Path) Probe(ctx context.Context) error {
	m := p.m
	m.mu.Lock()
	if p.closed {
		m.mu.Unlock()
		return ErrPathClosed
	}
	if m.active == p {
		m.mu.Unlock()
		return nil
	}
	wait := time.Until(m.lastSwitch.Add(m.switchSpacing()))
	m.mu.Unlock()
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}

	m.mu.Lock()
	if p.closed {
		m.mu.Unlock()
		return ErrPathClosed
	}
	if p.qp == nil {
		qp, err := m.conn.AddPath(p.tr)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		p.qp = qp
	}
	qp := p.qp
	m.mu.Unlock()

	if err := qp.Probe(ctx); err != nil {
		m.mu.Lock()
		if p.qp == qp {
			p.qp = nil
		}
		m.mu.Unlock()
		_ = qp.Close() // not actionable: the probe's error is the one to report
		if errors.Is(err, quic.ErrPathClosed) {
			return ErrPathClosed
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.qp != qp {
		return ErrPathClosed // closed while probing
	}
	p.validated = true
	return nil
}

// Switch moves the connection onto the path, validated by Probe. The path
// left is retired once the move takes effect.
func (p *Path) Switch() error {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.switchTo(p)
}

// switchTo moves the connection onto p. m.mu is held.
func (m *Migrator) switchTo(p *Path) error {
	if p.closed {
		return ErrPathClosed
	}
	if m.active == p {
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
	prev := m.active
	m.active = p
	m.lastSwitch = time.Now()
	m.leave(prev)
	m.wake(p.tr)
	return nil
}

// leave retires the quic-go path of prev, which the connection is leaving,
// once the switch has taken effect, and then gives up prev's socket if prev
// is closed. m.mu is held.
//
// Retiring the path matters beyond tidiness: the RETIRE_CONNECTION_ID it
// sends is the first packet a receive-only client sends on the new path, so
// it is what tells the server the client moved, and it returns a connection
// ID the server replaces for the next move.
func (m *Migrator) leave(prev *Path) {
	qp := prev.qp
	prev.qp, prev.validated = nil, false
	release := prev.closed
	if qp == nil {
		// The home path as dialed has no quic-go path to retire.
		if release {
			m.release(prev)
		}
		return
	}
	go func() {
		m.retire(qp)
		if release {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.release(prev)
		}
	}()
}

// retire closes qp as soon as the connection is no longer on it: quic-go
// refuses to close the active path, and a switch takes effect only at the
// connection's next event. It ends when qp is closed or the connection is.
func (m *Migrator) retire(qp *quic.Path) {
	ticker := time.NewTicker(retirePollInterval)
	defer ticker.Stop()
	for qp.Close() != nil {
		select {
		case <-m.conn.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// retirePollInterval spaces the attempts to retire a path left.
const retirePollInterval = 5 * time.Millisecond

// wake makes the connection act on a switch now rather than at its next
// event. quic-go has no call for that, but abandoning a path schedules
// sending, so add a path over tr and abandon it before it is ever probed,
// which sends nothing for it.
func (m *Migrator) wake(tr *quic.Transport) {
	w, err := m.conn.AddPath(tr)
	if err != nil {
		return // the connection is gone: nothing to wake
	}
	_ = w.Close() // not actionable: a path never probed closes without error
}

// Close gives up the path and its socket. Closing the active path, whose
// network is gone, does not wait for another path to validate: the path
// moves onto a new socket bound to no network, which the OS routes over its
// default network, and the server follows the connection there as it
// follows any client whose address changes. On a network just brought up
// that is far quicker than a probe, whose first packets are often lost.
func (p *Path) Close() error {
	m := p.m
	m.mu.Lock()
	defer m.mu.Unlock()
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
	if qp := p.qp; qp != nil {
		p.qp, p.validated = nil, false
		_ = qp.Close() // not actionable: a path not active closes without error
	}
	m.release(p)
	return nil
}

// release gives up p's socket and keeps its transport for the next path
// added. m.mu is held.
func (m *Migrator) release(p *Path) {
	p.slot.clear()
	m.spare = append(m.spare, p)
}

// switchSpacing is the least time between a switch and the next probe: the
// round trip that replaces the retired connection ID (see minSwitchSpacing).
// m.mu is held.
func (m *Migrator) switchSpacing() time.Duration {
	return max(minSwitchSpacing, 2*m.conn.ConnectionStats().SmoothedRTT)
}

// close closes the socket and transport of every path added. The home
// path's are its dialer's to close.
func (m *Migrator) close() {
	m.mu.Lock()
	added := m.added
	m.added = nil
	m.mu.Unlock()
	for _, p := range added {
		_ = p.slot.Close() // not actionable: the connection is gone
		_ = p.tr.Close()   // not actionable: as above
	}
}

// zeroConnIDs generates zero-length connection IDs.
type zeroConnIDs struct{}

var _ quic.ConnectionIDGenerator = zeroConnIDs{}

func (zeroConnIDs) GenerateConnectionID() (quic.ConnectionID, error) {
	return quic.ConnectionID{}, nil
}

func (zeroConnIDs) ConnectionIDLen() int { return 0 }
