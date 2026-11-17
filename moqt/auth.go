package moqt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

// Limits of the Auth Stream's fields.
const (
	// maxAuthExpires is the largest AUTH_OK Expires, in milliseconds.
	maxAuthExpires = 1<<53 - 1
	// maxAuthReasonLength is the longest AUTH_ERROR Reason Phrase, in bytes.
	maxAuthReasonLength = 8192
)

var (
	// ErrAuthNotSupported is returned by Session.Auth when the peer reset the
	// Auth Stream before answering, as a peer without the Auth Stream does.
	ErrAuthNotSupported = errors.New("moqt: peer does not support the auth stream")

	// ErrAuthEnded is returned by AuthWriter methods once the token's grant
	// has ended: refused, withdrawn by the presenter, or its handler returned.
	ErrAuthEnded = errors.New("moqt: auth ended")

	// ErrInvalidGrant is a grant AuthWriter.Grant does not take for a reason
	// other than its root or patterns: a negative MaxAge, or one too large
	// for one AUTH_OK.
	ErrInvalidGrant = errors.New("moqt: invalid grant")
)

// Grant is what an acceptor allows for one token: a root, the directory of
// broadcast paths it applies to, and beneath it the paths the presenter may
// publish to the acceptor and subscribe to from it.
//
// Root is a prefix of broadcast paths relative to the session, such as
// "/room/1/". The presenter may request announcements for a prefix only when
// the prefix lies within the root of a grant in force, as a directory is
// listed only by one who may read it. A grant with an empty Root grants
// nothing.
//
// Publish and Subscribe are path patterns relative to Root: with Root
// "/room/1/", "/alice" is "/room/1/alice". A pattern matches broadcast paths
// the way an http.ServeMux pattern matches URL paths, and the way
// Session.AcceptAnnounce reads paths and prefixes: "/alice" matches that path
// alone, a pattern ending in "/" matches every path beneath it, and "/"
// matches every path beneath the root. A segment may be a wildcard. "{name}"
// matches any one non-empty segment, so "/{user}/audio" matches
// "/alice/audio". "{name...}" as the last segment
// matches the rest of the path, even an empty rest, and "{$}" as the last
// segment matches only the "/" before it. A name is letters, digits and "_",
// not starting with a digit, as in net/http, and unique within a pattern. An
// empty segment, as in "/room//x", is a literal one.
type Grant struct {
	// Root is the directory the grant applies to, ending in "/".
	Root string
	// Publish matches the paths the presenter may announce and serve.
	Publish []string
	// Subscribe matches the paths the presenter may subscribe to and fetch.
	Subscribe []string
	// MaxAge is how long the grant lasts, or 0 for no limit. It tells the
	// presenter when to present a replacement token, at millisecond precision.
	// Unless the handler grants again first, the grant is revoked with
	// UNAUTHORIZED when it lapses.
	MaxAge time.Duration
}

func (g Grant) message() message.AuthOkMessage {
	m := message.AuthOkMessage{
		Root:              g.Root,
		PublishPatterns:   g.Publish,
		SubscribePatterns: g.Subscribe,
		Expires:           uint64(g.MaxAge.Milliseconds()),
	}
	if g.MaxAge > 0 && m.Expires == 0 {
		// Below a millisecond, the grant lapses at once rather than never.
		m.Expires = 1
	}
	return m
}

func grantFromMessage(m message.AuthOkMessage) (Grant, error) {
	g := Grant{
		Root:      m.Root,
		Publish:   m.PublishPatterns,
		Subscribe: m.SubscribePatterns,
		MaxAge:    time.Duration(m.Expires) * time.Millisecond,
	}
	if m.Expires > math.MaxInt64/uint64(time.Millisecond) {
		// Beyond what a time.Duration holds, some 292 years.
		g.MaxAge = math.MaxInt64
	}
	if _, err := parseGrant(g); err != nil {
		return Grant{}, err
	}
	return g, nil
}

// scopeGrant is a grant parsed, as a session's scope holds it: its root, and
// its patterns joined to the root. It shares nothing with the Grant, so the
// caller may reuse its slices.
type scopeGrant struct {
	root               prefix
	publish, subscribe []PathPattern
}

// sameScope reports whether g and h allow the same, whatever their max age.
func (g scopeGrant) sameScope(h scopeGrant) bool {
	same := func(p, q PathPattern) bool { return p.raw == q.raw }
	return g.root == h.root &&
		slices.EqualFunc(g.publish, h.publish, same) && slices.EqualFunc(g.subscribe, h.subscribe, same)
}

// Validate reports whether g is a grant AuthWriter.Grant takes: a root that is
// a prefix, or empty with no patterns, patterns that parse beneath it, a
// MaxAge that is not negative, and a size that fits one AUTH_OK. An error
// wraps ErrInvalidPathPattern for the root or a pattern, and ErrInvalidGrant
// otherwise.
func (g Grant) Validate() error {
	_, _, err := g.check()
	return err
}

// errGrantTooLarge is a grant too large for one AUTH_OK, which AuthWriter.Grant
// answers with NOT_SUPPORTED.
var errGrantTooLarge = fmt.Errorf("%w: too large for AUTH_OK", ErrInvalidGrant)

// check is the one check AuthWriter.Grant and Validate make: it parses g and
// encodes the AUTH_OK that grants it.
func (g Grant) check() (scopeGrant, message.AuthOkMessage, error) {
	if g.MaxAge < 0 {
		return scopeGrant{}, message.AuthOkMessage{}, fmt.Errorf("%w: negative MaxAge %v", ErrInvalidGrant, g.MaxAge)
	}
	sg, err := parseGrant(g)
	if err != nil {
		return scopeGrant{}, message.AuthOkMessage{}, err
	}
	// A non-negative time.Duration in milliseconds stays below maxAuthExpires.
	m := g.message()
	if n := m.Len(); n > message.MaxMessageSize {
		return scopeGrant{}, message.AuthOkMessage{}, fmt.Errorf("%w: %d bytes", errGrantTooLarge, n)
	}
	return sg, m, nil
}

// parseGrant parses g, reporting ErrInvalidPathPattern for a root that is not
// a prefix, or a pattern that is not one.
func parseGrant(g Grant) (scopeGrant, error) {
	if g.Root == "" {
		if len(g.Publish) > 0 || len(g.Subscribe) > 0 {
			return scopeGrant{}, fmt.Errorf("%w: patterns without a root", ErrInvalidPathPattern)
		}
		return scopeGrant{}, nil
	}
	if !isValidGrantRoot(g.Root) {
		return scopeGrant{}, fmt.Errorf("%w: root %q", ErrInvalidPathPattern, g.Root)
	}
	publish, err := parsePatterns(g.Root, g.Publish)
	if err != nil {
		return scopeGrant{}, err
	}
	subscribe, err := parsePatterns(g.Root, g.Subscribe)
	if err != nil {
		return scopeGrant{}, err
	}
	return scopeGrant{root: g.Root, publish: publish, subscribe: subscribe}, nil
}

// parsePatterns parses each of ss joined to root.
func parsePatterns(root prefix, ss []string) ([]PathPattern, error) {
	patterns := make([]PathPattern, len(ss))
	for i, s := range ss {
		p, err := joinPattern(root, s)
		if err != nil {
			return nil, err
		}
		patterns[i] = p
	}
	return patterns, nil
}

func matchAny(patterns []PathPattern, path BroadcastPath) bool {
	for _, p := range patterns {
		if p.Match(path) {
			return true
		}
	}
	return false
}

// AuthHandler verifies the tokens a peer presents and grants what each allows.
//
// ServeAuth is called once for each Auth Stream the peer opens, and once for
// the credential the connection carried, with an empty Token, when the
// session starts. The grant lasts while ServeAuth runs: it ends when ServeAuth
// returns, so a handler that grants waits on r.Context(), which ends when the
// presenter withdraws the token or the session ends. A handler may call
// w.Grant again to replace the grant, and w.Refuse to revoke it.
//
// The session's scope is the union of every grant in force, and it enforces
// it: a request the scope does not cover is refused with UNAUTHORIZED, and
// when the scope shrinks, what it no longer covers is cancelled while the
// session stays up. A session with no AuthHandler enforces nothing.
type AuthHandler interface {
	ServeAuth(w *AuthWriter, r *AuthRequest)
}

// AuthHandlerFunc adapts a function to an AuthHandler.
type AuthHandlerFunc func(w *AuthWriter, r *AuthRequest)

func (f AuthHandlerFunc) ServeAuth(w *AuthWriter, r *AuthRequest) {
	f(w, r)
}

// AuthRequest is a token presented by the peer.
type AuthRequest struct {
	// Token is the credential to verify, opaque to moqt. It is empty for the
	// credential the connection itself carried, such as a query in the
	// request URI, a client certificate, or nothing.
	Token []byte

	// RequestURI is the session's request URI (see Session.RequestURI). A
	// grant's root is relative to its path.
	RequestURI string

	ctx context.Context
}

// Context returns the request's context. It ends when the presenter
// withdraws the token, or when the session ends.
func (r *AuthRequest) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// AuthWriter answers one token.
type AuthWriter struct {
	scope *authScope
	// stream is the Auth Stream, or nil for the connection's own credential,
	// whose grant is applied without a reply.
	stream transport.Stream

	mu    sync.Mutex
	ended bool
	// expiry revokes the grant when it lapses; generation tells it whether
	// the grant it was set for is still the one in force.
	expiry     *time.Timer
	generation uint64
}

// Grant grants g for the token, replacing any earlier grant: it applies g to
// the session's scope, then sends AUTH_OK. A grant too large for one AUTH_OK
// is never trimmed: the token is refused with NOT_SUPPORTED instead, and the
// returned error says so.
func (w *AuthWriter) Grant(g Grant) error {
	sg, m, err := g.check()
	tooLarge := errors.Is(err, errGrantTooLarge)
	if err != nil && !tooLarge {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		return ErrAuthEnded
	}

	if tooLarge {
		return errors.Join(err, w.refuseLocked(NotSupportedErrorCode, "grant too large"))
	}

	w.scope.set(w, sg)
	w.expireAfterLocked(g.MaxAge)
	if w.stream == nil {
		return nil
	}
	if err := m.Encode(w.stream); err != nil {
		return fmt.Errorf("moqt: send AUTH_OK: %w", err)
	}
	return nil
}

// expireAfterLocked revokes the grant just set once d passes, or never for 0, in
// place of any earlier expiry. Caller MUST hold w.mu.
func (w *AuthWriter) expireAfterLocked(d time.Duration) {
	w.generation++
	if w.expiry != nil {
		w.expiry.Stop()
		w.expiry = nil
	}
	if d <= 0 {
		return
	}
	generation := w.generation
	w.expiry = time.AfterFunc(d, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.ended || w.generation != generation {
			return
		}
		// not actionable: the grant is revoked either way, and a failure to
		// tell the presenter is the stream failing.
		_ = w.refuseLocked(UnauthorizedSessionErrorCode, "grant expired")
	})
}

// Refuse refuses the token, or revokes its grant, with a session error code
// such as UnauthorizedSessionErrorCode, and ends it.
func (w *AuthWriter) Refuse(code SessionErrorCode, reason string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		return ErrAuthEnded
	}
	return w.refuseLocked(code, reason)
}

// refuseLocked ends the token with AUTH_ERROR. Caller MUST hold w.mu.
func (w *AuthWriter) refuseLocked(code SessionErrorCode, reason string) error {
	w.endLocked()
	if w.stream == nil {
		return nil
	}
	return sendAuthError(w.stream, code, reason)
}

// sendAuthError ends stream with AUTH_ERROR.
func sendAuthError(stream transport.Stream, code SessionErrorCode, reason string) error {
	err := message.AuthErrorMessage{Code: uint64(code), Reason: authReason(reason)}.Encode(stream)
	_ = stream.Close() // not actionable: the stream is done either way
	if err != nil {
		return fmt.Errorf("moqt: send AUTH_ERROR: %w", err)
	}
	return nil
}

// end ends the token's grant, if it has not ended, once its handler
// returned or its presenter withdrew it.
func (w *AuthWriter) end() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.ended {
		w.endLocked()
	}
}

// authReason makes reason fit AUTH_ERROR, so a refusal always carries its
// code: invalid UTF-8 is replaced, and a reason too long is cut at a
// character boundary.
func authReason(reason string) string {
	reason = strings.ToValidUTF8(reason, "�")
	if len(reason) <= maxAuthReasonLength {
		return reason
	}
	cut := maxAuthReasonLength
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut]
}

// endLocked removes the grant from the scope. Caller MUST hold w.mu.
func (w *AuthWriter) endLocked() {
	w.ended = true
	if w.expiry != nil {
		w.expiry.Stop()
		w.expiry = nil
	}
	w.scope.remove(w)
}

// safeServeAuth runs handler, recovering a panic, and ends the grant when it
// returns.
func safeServeAuth(handler AuthHandler, w *AuthWriter, r *AuthRequest) (err error) {
	defer w.end()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic during auth handling: %v", p)
		}
	}()
	handler.ServeAuth(w, r)
	return nil
}

// authScope is a session's scope: the union of the grants in force on it.
type authScope struct {
	mu     sync.RWMutex
	grants map[*AuthWriter]scopeGrant

	// conn answers the credential the connection itself carried. ready is
	// closed once it has, so requests that arrive first are judged against
	// its grant.
	conn      *AuthWriter
	ready     chan struct{}
	readyOnce sync.Once

	// changed is called, without mu, after the union changed.
	changed func()

	// watchers are the announce streams the scope applies to, which rescope
	// on each change. They have their own lock, apart from the grants that
	// every request reads.
	watchersMu sync.Mutex
	watchers   map[scopeWatcher]struct{}
}

// scopeWatcher re-applies the scope after it changed.
type scopeWatcher interface {
	rescope()
}

// watch makes w rescope on each change of the scope until the returned func
// is called.
func (s *authScope) watch(w scopeWatcher) (unwatch func()) {
	s.watchersMu.Lock()
	if s.watchers == nil {
		s.watchers = make(map[scopeWatcher]struct{})
	}
	s.watchers[w] = struct{}{}
	s.watchersMu.Unlock()
	return func() {
		s.watchersMu.Lock()
		delete(s.watchers, w)
		s.watchersMu.Unlock()
	}
}

// watching returns the watchers there are now.
func (s *authScope) watching() []scopeWatcher {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	ws := make([]scopeWatcher, 0, len(s.watchers))
	for w := range s.watchers {
		ws = append(ws, w)
	}
	return ws
}

func newAuthScope(changed func()) *authScope {
	s := &authScope{
		grants:  make(map[*AuthWriter]scopeGrant),
		ready:   make(chan struct{}),
		changed: changed,
	}
	s.conn = &AuthWriter{scope: s}
	return s
}

func (s *authScope) set(w *AuthWriter, g scopeGrant) {
	s.mu.Lock()
	old, had := s.grants[w]
	s.grants[w] = g
	s.mu.Unlock()
	s.answered(w)
	// A grant sent again to extend it changes nothing.
	if !had || !old.sameScope(g) {
		s.changed()
	}
}

func (s *authScope) remove(w *AuthWriter) {
	s.mu.Lock()
	_, had := s.grants[w]
	delete(s.grants, w)
	s.mu.Unlock()
	s.answered(w)
	if had {
		s.changed()
	}
}

func (s *authScope) answered(w *AuthWriter) {
	if w == s.conn {
		s.readyOnce.Do(func() { close(s.ready) })
	}
}

// wait waits until the connection's own credential was answered, and reports
// false if ctx ends first.
func (s *authScope) wait(ctx context.Context) bool {
	if s == nil {
		return true
	}
	select {
	case <-s.ready:
		return true
	case <-ctx.Done():
		return false
	}
}

// canList reports whether pfx lies within the root of a grant in force, so
// the peer may request announcements for it. A nil scope enforces nothing.
func (s *authScope) canList(pfx prefix) bool {
	return s.anyGrant(func(g scopeGrant) bool { return g.root != "" && strings.HasPrefix(pfx, g.root) })
}

// canPublish reports whether a grant in force lets the peer publish path. A
// nil scope enforces nothing.
func (s *authScope) canPublish(path BroadcastPath) bool {
	return s.anyGrant(func(g scopeGrant) bool { return matchAny(g.publish, path) })
}

// canPublishBeneath reports whether a grant in force lets the peer publish
// every path beneath path, as an announcement of path claims under
// moq-lite-06. A nil scope enforces nothing.
func (s *authScope) canPublishBeneath(path BroadcastPath) bool {
	return s.anyGrant(func(g scopeGrant) bool {
		for _, p := range g.publish {
			if p.coversBeneath(path) {
				return true
			}
		}
		return false
	})
}

// canSubscribe reports whether a grant in force lets the peer subscribe to
// path. A nil scope enforces nothing.
func (s *authScope) canSubscribe(path BroadcastPath) bool {
	return s.anyGrant(func(g scopeGrant) bool { return matchAny(g.subscribe, path) })
}

// anyGrant reports whether ok holds for a grant in force. A nil scope
// enforces nothing, so it reports true.
func (s *authScope) anyGrant(ok func(scopeGrant) bool) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.grants {
		if ok(g) {
			return true
		}
	}
	return false
}

// AuthError is a token refused, or a grant revoked, by the peer.
type AuthError struct {
	Code   SessionErrorCode
	Reason string
}

func (err *AuthError) Error() string {
	text := err.Code.String()
	if text == "" {
		text = fmt.Sprintf("moqt: auth error %#x", uint32(err.Code))
	}
	if err.Reason != "" {
		return text + ": " + err.Reason
	}
	return text
}

// Credential is a token presented to the peer, and what the peer granted for
// it. It lasts until Close withdraws the token, the peer revokes it, or the
// session ends.
type Credential struct {
	stream transport.Stream
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu    sync.Mutex
	grant Grant
}

// Grant returns the latest grant the peer sent for the token.
func (c *Credential) Grant() Grant {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := c.grant
	g.Publish = slices.Clone(g.Publish)
	g.Subscribe = slices.Clone(g.Subscribe)
	return g
}

// Context returns a context that is canceled when the credential's grant
// ends. Its cause, from context.Cause, is an *AuthError when the peer revoked
// the grant, and the session's cause when the session ended.
func (c *Credential) Context() context.Context {
	return c.ctx
}

// Close withdraws the token, which ends its grant on the peer.
func (c *Credential) Close() error {
	return c.stream.Close()
}

// readReplies follows the peer's answers after the first: a further AUTH_OK
// replaces the grant, and AUTH_ERROR or the end of the stream ends it.
func (c *Credential) readReplies(violation func(string)) {
	for {
		g, err := readAuthReply(c.stream)
		if err != nil {
			var cause error
			var authErr *AuthError
			var connErr *transport.ApplicationError
			switch {
			case errors.As(err, &authErr):
				cause = authErr
			case errors.As(err, &connErr):
				// The session ended; its context may not have yet.
				cause = connErr
			case errors.Is(err, io.EOF):
			case isProtocolViolation(err):
				violation(err.Error())
			}
			cancelStreamWithError(c.stream, authStreamCancelled)
			c.cancel(cause)
			return
		}
		c.mu.Lock()
		c.grant = g
		c.mu.Unlock()
	}
}

// readAuthReply reads one acceptor message: the grant of an AUTH_OK, or an
// *AuthError for AUTH_ERROR.
func readAuthReply(r io.Reader) (Grant, error) {
	t, err := message.ReadAuthReplyType(r)
	if err != nil {
		return Grant{}, err
	}
	switch t {
	case message.AuthReplyOK:
		var m message.AuthOkMessage
		if err := m.Decode(r); err != nil {
			return Grant{}, decodeViolation("AUTH_OK", err)
		}
		if m.Expires > maxAuthExpires {
			return Grant{}, fmt.Errorf("%w: AUTH_OK expires %d ms", errAuthViolation, m.Expires)
		}
		g, err := grantFromMessage(m)
		if err != nil {
			return Grant{}, err
		}
		return g, nil
	case message.AuthReplyError:
		var m message.AuthErrorMessage
		if err := m.Decode(r); err != nil {
			return Grant{}, decodeViolation("AUTH_ERROR", err)
		}
		if m.Code > math.MaxUint32 || len(m.Reason) > maxAuthReasonLength || !utf8.ValidString(m.Reason) {
			return Grant{}, fmt.Errorf("%w: malformed AUTH_ERROR", errAuthViolation)
		}
		return Grant{}, &AuthError{Code: SessionErrorCode(m.Code), Reason: m.Reason}
	default:
		return Grant{}, fmt.Errorf("%w: auth reply type %#x", errAuthViolation, uint64(t))
	}
}

// errAuthViolation marks an Auth Stream message that breaks the protocol,
// which closes the session.
var errAuthViolation = errors.New("moqt: auth stream protocol violation")

// decodeViolation classifies a decode error: the stream ending or being reset
// is not the peer's message, while anything else it sent wrong is a protocol
// violation.
func decodeViolation(what string, err error) error {
	if _, ok := errors.AsType[*transport.StreamError](err); ok || errors.Is(err, io.EOF) {
		return err
	}
	return fmt.Errorf("%w: %s: %w", errAuthViolation, what, err)
}

func isProtocolViolation(err error) bool {
	return errors.Is(err, errAuthViolation) || errors.Is(err, ErrInvalidPathPattern)
}

// authStreamCancelled resets the read side of an Auth Stream whose token is
// over, so a presenter that never closes its side does not hold it open.
const authStreamCancelled transport.StreamErrorCode = 0x1

// Auth presents token to the peer on a new Auth Stream and waits for its
// first answer. An empty token presents the credential the connection already
// carried. It returns an *AuthError when the peer refuses the token, and
// ErrAuthNotSupported when the peer does not support the Auth Stream.
//
// The grant lasts until Credential.Close withdraws the token, the peer
// revokes it, or the session ends. ctx bounds only the wait for the answer.
func (sess *Session) Auth(ctx context.Context, token []byte) (*Credential, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}

	if sess.closed.Load() {
		return nil, ErrClosedSession
	}

	stream, err := sess.openStream(ctx, "auth")
	if err != nil {
		return nil, err
	}

	if err := message.StreamTypeAuth.Encode(stream); err != nil {
		cancelStreamWithError(stream, authStreamCancelled)
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}
	if err := (message.AuthMessage{Token: token}).Encode(stream); err != nil {
		cancelStreamWithError(stream, authStreamCancelled)
		return nil, fmt.Errorf("failed to encode AUTH message: %w", err)
	}

	stop := context.AfterFunc(ctx, func() {
		cancelStreamWithError(stream, authStreamCancelled)
	})
	g, err := readAuthReply(stream)
	if !stop() {
		return nil, abandonedErr("auth", ctx)
	}
	if err != nil {
		cancelStreamWithError(stream, authStreamCancelled)
		if _, ok := errors.AsType[*AuthError](err); ok {
			return nil, err
		}
		if _, ok := errors.AsType[*transport.StreamError](err); ok {
			return nil, ErrAuthNotSupported
		}
		if errors.Is(err, io.EOF) {
			return nil, ErrAuthEnded
		}
		if isProtocolViolation(err) {
			sess.terminateProtocolViolation(err.Error())
		}
		return nil, fmt.Errorf("failed to read auth reply: %w", err)
	}

	cctx, cancel := context.WithCancelCause(sess.ctx)
	c := &Credential{stream: stream, ctx: cctx, cancel: cancel, grant: g}
	sess.wg.Go(func() {
		c.readReplies(sess.terminateProtocolViolation)
	})
	return c, nil
}

// handleAuthStream reads the peer's AUTH and serves it to the AuthHandler for
// as long as the token applies.
func (sess *Session) handleAuthStream(stream transport.Stream) {
	var am message.AuthMessage
	if err := am.Decode(stream); err != nil {
		if err := decodeViolation("AUTH", err); isProtocolViolation(err) {
			sess.terminateProtocolViolation(err.Error())
		}
		cancelStreamWithError(stream, authStreamCancelled)
		return
	}

	if sess.authHandler == nil {
		if err := sendAuthError(stream, NotSupportedErrorCode, "tokens are not verified in band"); err != nil {
			sess.logError("failed to refuse a token", err)
		}
		stream.CancelRead(authStreamCancelled)
		return
	}

	reqCtx, cancelReq := sess.streamContext(stream)
	defer cancelReq()
	reqCtx, withdraw := context.WithCancel(reqCtx)
	defer withdraw()

	w := &AuthWriter{scope: sess.scope, stream: stream}

	// The presenter sends nothing after AUTH: the end of its side withdraws
	// the token, and anything more is a protocol violation.
	sess.wg.Go(func() {
		var b [1]byte
		n, _ := stream.Read(b[:])
		if n > 0 {
			sess.terminateProtocolViolation("message after AUTH")
		}
		w.end()
		withdraw()
	})

	r := &AuthRequest{Token: am.Token, RequestURI: sess.path, ctx: reqCtx}
	if err := safeServeAuth(sess.authHandler, w, r); err != nil {
		sess.logError("auth handler error", err)
	}
	stream.CancelRead(authStreamCancelled)
}

// mayServe reports whether the peer's scope lets it subscribe to path, waiting
// first for the connection's own grant.
func (sess *Session) mayServe(path BroadcastPath) bool {
	if sess.scope == nil {
		return true
	}
	return sess.scope.wait(sess.ctx) && sess.scope.canSubscribe(path)
}

// requestRescope asks rescopeLoop to apply a change of the scope. It never
// blocks: changes made while a pass runs are applied by one more pass.
func (sess *Session) requestRescope() {
	select {
	case sess.rescopeCh <- struct{}{}:
	default:
	}
}

// rescopeLoop applies scope changes one pass at a time until the session
// ends, so a grant or refusal never waits on a slow peer stream, and two
// passes never interleave.
func (sess *Session) rescopeLoop() {
	for {
		select {
		case <-sess.rescopeCh:
			sess.rescope()
		case <-sess.ctx.Done():
			return
		}
	}
}

// rescope applies a change of the peer's scope: it cancels each subscription
// the scope no longer covers, and each announce stream whose prefix no longer
// lies within a root, with UNAUTHORIZED, and re-filters the announcements the
// peer publishes. A fetch being served is one group, judged when requested,
// and runs to its end.
func (sess *Session) rescope() {
	sess.trackWriterMapLocker.RLock()
	var revoked []*TrackWriter
	for _, tw := range sess.trackWriters {
		if !sess.scope.canSubscribe(tw.BroadcastPath) {
			revoked = append(revoked, tw)
		}
	}
	sess.trackWriterMapLocker.RUnlock()
	// CloseWithError removes the writer from the map itself.
	for _, tw := range revoked {
		tw.CloseWithError(SubscribeErrorCodeUnauthorized)
	}

	for _, w := range sess.scope.watching() {
		w.rescope()
	}
}
