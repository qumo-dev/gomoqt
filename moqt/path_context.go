package moqt

import "context"

// pathContextKey is the context key under which the native-QUIC router stashes
// the request path learned from the client's SETUP message. It is the
// native-QUIC analog of the WebTransport handler's r.URL.Path: the path is
// resolved above Session (by the binding), not inside it.
type pathContextKey struct{}

// withPathContext returns a StreamConn whose Context derives from conn's and
// carries path under pathContextKey. Used by the native-QUIC server router so
// handlers can recover the request path via PathFromContext.
func withPathContext(conn StreamConn, path string) StreamConn {
	return &pathContextConn{
		StreamConn: conn,
		ctx:        context.WithValue(conn.Context(), pathContextKey{}, path),
	}
}

// PathFromContext returns the native-QUIC request path stored on the context by
// the server router. On the client side, or for WebTransport sessions, it
// returns ("", false) — WebTransport handlers read the path from r.URL.Path on
// the HTTP request instead.
//
// Use it as: path, ok := moqt.PathFromContext(sess.Context())
func PathFromContext(ctx context.Context) (string, bool) {
	path, ok := ctx.Value(pathContextKey{}).(string)
	return path, ok && path != ""
}

// pathContextConn wraps a StreamConn to override its Context so the router can
// attach the request path. It mirrors the streamConnContext pattern in
// server.go.
type pathContextConn struct {
	StreamConn
	ctx context.Context
}

func (c *pathContextConn) Context() context.Context { return c.ctx }
