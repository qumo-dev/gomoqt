package qmuxgo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/okdaichi/qmux-go/qmux"
	"github.com/qumo-dev/gomoqt/transport"
)

// DefaultKeepAlivePeriod is the interval between QX_PING frames when no
// configuration is given. The pings measure the round-trip time, find a
// peer that is gone, and keep proxies from closing an idle WebSocket.
const DefaultKeepAlivePeriod = 10 * time.Second

// UpgradeError reports why a request was not upgraded.
type UpgradeError struct {
	// Status is the HTTP status to answer with. It is zero when the
	// response has already been written.
	Status int
	Err    error
}

func (e *UpgradeError) Error() string { return "qmuxgo: upgrade: " + e.Err.Error() }
func (e *UpgradeError) Unwrap() error { return e.Err }

// Upgrader upgrades HTTP requests to QMux sessions over WebSocket.
type Upgrader struct {
	// CheckOrigin reports whether the request's Origin is acceptable.
	// Browsers do not apply CORS to WebSocket, so this check is the only
	// one. If nil, only same-origin requests are accepted.
	CheckOrigin func(r *http.Request) bool
	// Protocols lists the application protocols accepted, in order of
	// preference. A client offers each as "qmux-02.<protocol>".
	Protocols []string
	// Config configures the QMux connection. If nil, the defaults apply,
	// with DefaultKeepAlivePeriod.
	Config *qmux.Config
}

// Upgrade upgrades the request and waits for the client's QMux transport
// parameters. A request that names none of Protocols is refused: without a
// negotiated subprotocol neither side knows what the other speaks.
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (transport.WebTransportSession, error) {
	if !isWebSocketUpgrade(r) {
		return nil, &UpgradeError{Status: http.StatusUpgradeRequired, Err: errors.New("not a WebSocket upgrade")}
	}
	if u.CheckOrigin != nil && !u.CheckOrigin(r) {
		return nil, &UpgradeError{Status: http.StatusForbidden, Err: errors.New("origin not allowed")}
	}
	protocol, ok := selectProtocol(r.Header.Values("Sec-WebSocket-Protocol"), u.Protocols)
	if !ok {
		return nil, &UpgradeError{Status: http.StatusBadRequest, Err: errors.New("no supported subprotocol offered")}
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{Version + "." + protocol},
		// With CheckOrigin set the origin has been checked above;
		// without it Accept applies its same-origin rule.
		InsecureSkipVerify: u.CheckOrigin != nil,
	})
	if err != nil {
		// Accept has answered the request.
		return nil, &UpgradeError{Err: err}
	}

	config := configOrDefault(u.Config)
	mc := newMessageConn(ws, config, localAddrOf(r), parseAddr(r.RemoteAddr))
	conn, err := qmux.ServerMessages(r.Context(), mc, config)
	if err != nil {
		return nil, &UpgradeError{Err: err}
	}
	return &session{conn: conn, protocol: protocol, tls: r.TLS}, nil
}

// Dial opens a WebSocket to rawURL ("wss://" or "ws://") and starts a QMux
// session on it, offering protocols as application protocols.
func Dial(ctx context.Context, rawURL string, header http.Header, tlsConfig *tls.Config, protocols []string, config *qmux.Config) (*http.Response, transport.WebTransportSession, error) {
	if tlsConfig != nil {
		// The WebSocket handshake is HTTP/1.1, whatever else the
		// configuration is used to dial.
		tlsConfig = tlsConfig.Clone()
		tlsConfig.NextProtos = []string{"http/1.1"}
	}
	offered := make([]string, len(protocols))
	for i, p := range protocols {
		offered[i] = Version + "." + p
	}
	ws, rsp, err := websocket.Dial(ctx, rawURL, &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
		HTTPHeader:   header,
		Subprotocols: offered,
	})
	if err != nil {
		return rsp, nil, fmt.Errorf("qmuxgo: dial: %w", err)
	}
	protocol, ok := strings.CutPrefix(ws.Subprotocol(), Version+".")
	if !ok || !slices.Contains(protocols, protocol) {
		_ = ws.CloseNow() // not actionable: the connection is refused
		return rsp, nil, fmt.Errorf("qmuxgo: dial: server selected subprotocol %q, which was not offered", ws.Subprotocol())
	}

	config = configOrDefault(config)
	mc := newMessageConn(ws, config, addr("local"), addr(rsp.Request.URL.Host))
	conn, err := qmux.DialMessages(ctx, mc, config)
	if err != nil {
		return rsp, nil, fmt.Errorf("qmuxgo: dial: %w", err)
	}
	return rsp, &session{conn: conn, protocol: protocol, tls: rsp.TLS}, nil
}

func configOrDefault(config *qmux.Config) *qmux.Config {
	if config != nil {
		return config
	}
	return &qmux.Config{KeepAlivePeriod: DefaultKeepAlivePeriod}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		headerHasToken(r.Header, "Connection", "upgrade") &&
		headerHasToken(r.Header, "Upgrade", "websocket")
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// selectProtocol returns the first of the supported application protocols
// that the client offered as "qmux-02.<protocol>".
func selectProtocol(offered, supported []string) (string, bool) {
	var offers []string
	for _, value := range offered {
		for part := range strings.SplitSeq(value, ",") {
			offers = append(offers, strings.TrimSpace(part))
		}
	}
	for _, p := range supported {
		if slices.Contains(offers, Version+"."+p) {
			return p, true
		}
	}
	return "", false
}

var _ qmux.MessageConn = (*messageConn)(nil)

// messageConn carries QMux records as binary WebSocket messages.
type messageConn struct {
	ws     *websocket.Conn
	buf    []byte
	local  net.Addr
	remote net.Addr

	closeOnce sync.Once
}

func newMessageConn(ws *websocket.Conn, config *qmux.Config, local, remote net.Addr) *messageConn {
	// QMux enforces max_record_size itself; the WebSocket limit only has
	// to let a full record through.
	limit := int64(max(config.MaxRecordSize, 16382)) + 1
	ws.SetReadLimit(limit)
	return &messageConn{ws: ws, local: local, remote: remote}
}

func (c *messageConn) ReadMessage() ([]byte, error) {
	// The connection's lifetime bounds the read: Close ends it.
	typ, r, err := c.ws.Reader(context.Background())
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, errors.New("qmuxgo: text message on a QMux WebSocket")
	}
	c.buf = c.buf[:0]
	for {
		if len(c.buf) == cap(c.buf) {
			c.buf = append(c.buf, 0)[:len(c.buf)]
		}
		n, err := r.Read(c.buf[len(c.buf):cap(c.buf)])
		c.buf = c.buf[:len(c.buf)+n]
		if errors.Is(err, io.EOF) {
			return c.buf, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (c *messageConn) WriteMessage(p []byte) error {
	// The connection's lifetime bounds the write: Close ends it.
	return c.ws.Write(context.Background(), websocket.MessageBinary, p)
}

// Close starts the WebSocket closing handshake, which delivers what was
// written before it, and returns without waiting for the peer: the
// handshake ends by itself within seconds.
func (c *messageConn) Close() error {
	c.closeOnce.Do(func() {
		go func() {
			_ = c.ws.Close(websocket.StatusNormalClosure, "") // not actionable: the connection has ended
		}()
	})
	return nil
}

func (c *messageConn) LocalAddr() net.Addr  { return c.local }
func (c *messageConn) RemoteAddr() net.Addr { return c.remote }

// addr is an address known only as text.
type addr string

func (a addr) Network() string { return "websocket" }
func (a addr) String() string  { return string(a) }

func localAddrOf(r *http.Request) net.Addr {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return a
	}
	return addr("local")
}

func parseAddr(s string) net.Addr {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return net.TCPAddrFromAddrPort(ap)
	}
	return addr(s)
}
