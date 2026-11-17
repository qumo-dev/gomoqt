package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	authRoom1Path BroadcastPath = "/room/1/alice"
	authRoom2Path BroadcastPath = "/room/2/bob"
)

// roomTicketHandler grants each room's ticket the room's paths, refuses
// "bad", and grants the connection's own credential everything when the
// request URI carries "?admin" and nothing otherwise.
var roomTicketHandler = AuthHandlerFunc(func(w *AuthWriter, r *AuthRequest) {
	var g Grant
	switch string(r.Token) {
	case "":
		if u, err := url.Parse(r.RequestURI); err == nil && u.Query().Has("admin") {
			g = Grant{Root: "/", Subscribe: []string{"/"}}
		}
	case "room1":
		g = Grant{Root: "/room/1/", Subscribe: []string{"/"}}
	case "room2":
		g = Grant{Root: "/room/2/", Subscribe: []string{"/"}}
	case "publish":
		g = Grant{Root: "/room/1/", Publish: []string{"/"}}
	default:
		_ = w.Refuse(UnauthorizedSessionErrorCode, "unknown ticket")
		return
	}
	if err := w.Grant(g); err != nil {
		return
	}
	<-r.Context().Done()
})

// authGroupHandler opens a group every few milliseconds until the
// subscription ends.
func authGroupHandler(tw *TrackWriter) {
	ctx := tw.Context()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		gw, err := tw.OpenGroup(ctx)
		if err != nil {
			return
		}
		frame := NewFrame(1)
		_, _ = frame.Write([]byte{1})
		_ = gw.WriteFrame(frame)
		_ = gw.Close()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// authProbePath is the path startAuthServer dials to learn the server is up.
const authProbePath = "/probe"

// startAuthServer serves authRoom1Path and authRoom2Path behind handler. The
// handler on the server side receives every session it accepts.
func startAuthServer(t *testing.T, ctx context.Context, handler AuthHandler, sessions chan<- *Session) string {
	t.Helper()

	addr := freePort(t)
	mux := NewTrackMux(0)
	mux.PublishFunc(ctx, authRoom1Path, authGroupHandler)
	mux.PublishFunc(ctx, authRoom2Path, authGroupHandler)

	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{NextProtoMOQ},
			Certificates:       []tls.Certificate{generateTestCert(t)},
			InsecureSkipVerify: true,
		},
		QUICConfig:  &quic.Config{EnableDatagrams: true},
		TrackMux:    mux,
		AuthHandler: handler,
		FetchHandler: FetchHandlerFunc(func(w *GroupWriter, _ *FetchRequest) {
			frame := NewFrame(0)
			_, _ = frame.Write([]byte("fetched"))
			_ = w.WriteFrame(frame)
			_ = w.Close()
		}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			// The readiness probe below is not a session the test dialed.
			if sessions != nil && sess.RequestURI() != authProbePath {
				sessions <- sess
			}
			<-sess.Context().Done()
		}),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			t.Logf("server: %v", err)
		}
	}()
	t.Cleanup(func() { closeServer(t, srv) })

	require.Eventually(t, func() bool {
		sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+addr+authProbePath, nil)
		if err != nil {
			return false
		}
		_ = sess.CloseWithError(NoError, "probe")
		return true
	}, 10*time.Second, 50*time.Millisecond)

	return addr
}

func dialAuth(t *testing.T, ctx context.Context, url string, mux *TrackMux) *Session {
	t.Helper()
	sess, err := (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, url, mux)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "done") })
	return sess
}

// subscribeCode subscribes to path and returns the subscribe error code, or
// -1 when the subscription is accepted.
func subscribeCode(t *testing.T, ctx context.Context, sess *Session, path BroadcastPath) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tr, err := sess.Subscribe(ctx, path, "audio", nil)
	if err == nil {
		_ = tr.Close()
		return -1
	}
	var subErr *SubscribeError
	require.ErrorAs(t, err, &subErr)
	return int(subErr.SubscribeErrorCode())
}

func TestSession_Auth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, roomTicketHandler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)

	t.Run("a ticket returns its grant", func(t *testing.T) {
		cred, err := sess.Auth(ctx, []byte("room1"))
		require.NoError(t, err)
		defer func() { _ = cred.Close() }() // not actionable: the test is over

		g := cred.Grant()
		assert.Equal(t, "/room/1/", g.Root)
		assert.Equal(t, []string{"/"}, g.Subscribe)
		assert.Empty(t, g.Publish)
		assert.Zero(t, g.MaxAge)
	})

	t.Run("an unknown ticket is refused", func(t *testing.T) {
		_, err := sess.Auth(ctx, []byte("bad"))

		var authErr *AuthError
		require.ErrorAs(t, err, &authErr)
		assert.Equal(t, UnauthorizedSessionErrorCode, authErr.Code)
		assert.Equal(t, "unknown ticket", authErr.Reason)
	})

	t.Run("an empty token returns the connection's grant", func(t *testing.T) {
		cred, err := sess.Auth(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = cred.Close() }() // not actionable: the test is over

		assert.Empty(t, cred.Grant().Subscribe)
	})
}

func TestCredential_Grant_Regrant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	regrant := make(chan struct{})
	handler := AuthHandlerFunc(func(w *AuthWriter, r *AuthRequest) {
		if len(r.Token) == 0 {
			<-r.Context().Done()
			return
		}
		if err := w.Grant(Grant{Root: "/room/1/", Subscribe: []string{"/"}}); err != nil {
			return
		}
		select {
		case <-regrant:
		case <-r.Context().Done():
			return
		}
		if err := w.Grant(Grant{Root: "/room/2/", Subscribe: []string{"/"}}); err != nil {
			return
		}
		<-r.Context().Done()
	})
	addr := startAuthServer(t, ctx, handler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	cred, err := sess.Auth(ctx, []byte("room"))
	require.NoError(t, err)
	defer func() { _ = cred.Close() }() // not actionable: the test is over

	close(regrant)

	assert.Eventually(t, func() bool {
		g := cred.Grant()
		return g.Root == "/room/2/"
	}, 5*time.Second, 10*time.Millisecond, "a further AUTH_OK replaces the grant")
}

func TestCredential_Context(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, roomTicketHandler, nil)

	t.Run("Close withdrawing the token ends it", func(t *testing.T) {
		sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
		cred, err := sess.Auth(ctx, []byte("room1"))
		require.NoError(t, err)

		require.NoError(t, cred.Close())

		select {
		case <-cred.Context().Done():
		case <-ctx.Done():
			t.Fatal("credential did not end")
		}
		assert.ErrorIs(t, context.Cause(cred.Context()), context.Canceled)
	})

	t.Run("the session ending ends it with the session's cause", func(t *testing.T) {
		sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
		cred, err := sess.Auth(ctx, []byte("room1"))
		require.NoError(t, err)

		require.NoError(t, sess.CloseWithError(NoError, "done"))

		select {
		case <-cred.Context().Done():
		case <-ctx.Done():
			t.Fatal("credential did not end")
		}
		var connErr *transport.ApplicationError
		require.ErrorAs(t, context.Cause(cred.Context()), &connErr)
		assert.Equal(t, transport.ConnErrorCode(NoError), connErr.ErrorCode)
	})
}

func TestSession_Auth_NotSupported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, nil, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr, nil)

	_, err := sess.Auth(ctx, []byte("room1"))

	var authErr *AuthError
	require.ErrorAs(t, err, &authErr)
	assert.Equal(t, NotSupportedErrorCode, authErr.Code)
}

func TestSession_Auth_NoHandlerEnforcesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, nil, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr, nil)

	assert.Equal(t, -1, subscribeCode(t, ctx, sess, authRoom1Path))
}

func TestSession_Subscribe_Scope(t *testing.T) {
	tests := map[string]struct {
		url     string
		tickets []string
		path    BroadcastPath
		want    int
	}{
		"nothing granted":             {url: "/app", path: authRoom1Path, want: int(SubscribeErrorCodeUnauthorized)},
		"the room's ticket":           {url: "/app", tickets: []string{"room1"}, path: authRoom1Path, want: -1},
		"another room's ticket":       {url: "/app", tickets: []string{"room2"}, path: authRoom1Path, want: int(SubscribeErrorCodeUnauthorized)},
		"one of two tickets":          {url: "/app", tickets: []string{"room2", "room1"}, path: authRoom1Path, want: -1},
		"the connection's credential": {url: "/app?admin", path: authRoom2Path, want: -1},
		"a publish-only ticket":       {url: "/app", tickets: []string{"publish"}, path: authRoom1Path, want: int(SubscribeErrorCodeUnauthorized)},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			addr := startAuthServer(t, ctx, roomTicketHandler, nil)
			sess := dialAuth(t, ctx, "moqt://"+addr+tt.url, nil)
			for _, ticket := range tt.tickets {
				cred, err := sess.Auth(ctx, []byte(ticket))
				require.NoError(t, err)
				defer func() { _ = cred.Close() }() // not actionable: the test is over
			}

			got := subscribeCode(t, ctx, sess, tt.path)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSession_Fetch_Scope(t *testing.T) {
	tests := map[string]struct {
		path        BroadcastPath
		wantRefused bool
	}{
		"outside the scope": {path: authRoom2Path, wantRefused: true},
		"within the scope":  {path: authRoom1Path},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			addr := startAuthServer(t, ctx, roomTicketHandler, nil)
			sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
			cred, err := sess.Auth(ctx, []byte("room1"))
			require.NoError(t, err)
			defer func() { _ = cred.Close() }() // not actionable: the test is over

			gr, err := sess.Fetch((&FetchRequest{BroadcastPath: tt.path, TrackName: "audio"}).WithContext(ctx))
			require.NoError(t, err)
			frame := NewFrame(0)
			err = gr.ReadFrame(frame)

			if tt.wantRefused {
				var groupErr *GroupError
				require.ErrorAs(t, err, &groupErr)
				assert.Equal(t, UnauthorizedGroupErrorCode, groupErr.GroupErrorCode())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []byte("fetched"), frame.Body())
		})
	}
}

func TestSession_Auth_WithdrawCancelsOnlyItsSubscriptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, roomTicketHandler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	room1, err := sess.Auth(ctx, []byte("room1"))
	require.NoError(t, err)
	defer func() { _ = room1.Close() }() // not actionable: the test is over
	room2, err := sess.Auth(ctx, []byte("room2"))
	require.NoError(t, err)
	tr1, err := sess.Subscribe(ctx, authRoom1Path, "audio", nil)
	require.NoError(t, err)
	defer func() { _ = tr1.Close() }() // not actionable: the test is over
	tr2, err := sess.Subscribe(ctx, authRoom2Path, "audio", nil)
	require.NoError(t, err)
	defer func() { _ = tr2.Close() }() // not actionable: the test is over

	require.NoError(t, room2.Close())

	// Room 2's subscription ends with UNAUTHORIZED.
	var subErr *SubscribeError
	require.Eventually(t, func() bool {
		gr, err := tr2.AcceptGroup(ctx)
		if err == nil {
			gr.CancelRead(InternalGroupErrorCode)
			return false
		}
		return errors.As(err, &subErr)
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, SubscribeErrorCodeUnauthorized, subErr.SubscribeErrorCode())
	// Room 1's subscription and the session carry on.
	gctx, gcancel := context.WithTimeout(ctx, 2*time.Second)
	defer gcancel()
	gr, err := tr1.AcceptGroup(gctx)
	require.NoError(t, err)
	gr.CancelRead(InternalGroupErrorCode)
	assert.NoError(t, sess.Context().Err())
	// A new subscription to room 2 is refused.
	assert.Equal(t, int(SubscribeErrorCodeUnauthorized), subscribeCode(t, ctx, sess, authRoom2Path))
}

func TestSession_Auth_RefusalAfterGrantRevokes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	revoke := make(chan struct{})
	handler := AuthHandlerFunc(func(w *AuthWriter, r *AuthRequest) {
		if len(r.Token) == 0 {
			_ = w.Grant(Grant{})
			<-r.Context().Done()
			return
		}
		_ = w.Grant(Grant{Root: "/room/1/", Subscribe: []string{"/"}})
		select {
		case <-revoke:
			_ = w.Refuse(UnauthorizedSessionErrorCode, "key retired")
		case <-r.Context().Done():
		}
	})
	addr := startAuthServer(t, ctx, handler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	cred, err := sess.Auth(ctx, []byte("room1"))
	require.NoError(t, err)
	tr, err := sess.Subscribe(ctx, authRoom1Path, "audio", nil)
	require.NoError(t, err)
	defer func() { _ = tr.Close() }() // not actionable: the test is over

	close(revoke)

	select {
	case <-cred.Context().Done():
	case <-ctx.Done():
		t.Fatal("grant did not end")
	}
	var authErr *AuthError
	require.ErrorAs(t, context.Cause(cred.Context()), &authErr)
	assert.Equal(t, "key retired", authErr.Reason)
	assert.Equal(t, int(SubscribeErrorCodeUnauthorized), subscribeCode(t, ctx, sess, authRoom1Path))
	assert.NoError(t, sess.Context().Err())
}

func TestSession_AcceptAnnounce_PrefixWithinRoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, roomTicketHandler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	cred, err := sess.Auth(ctx, []byte("room1"))
	require.NoError(t, err)
	ar, err := sess.AcceptAnnounce("/room/1/")
	require.NoError(t, err)
	defer func() { _ = ar.Close() }() // not actionable: the test is over

	actx, acancel := context.WithTimeout(ctx, 5*time.Second)
	defer acancel()
	ann, err := ar.ReceiveAnnouncement(actx)
	require.NoError(t, err)
	assert.Equal(t, authRoom1Path, ann.BroadcastPath())

	// Withdrawing the ticket ends the stream, and with it room 1's announcement.
	require.NoError(t, cred.Close())
	select {
	case <-ann.Done():
	case <-ctx.Done():
		t.Fatal("announcement did not end")
	}
	_, err = ar.ReceiveAnnouncement(actx)
	var annErr *AnnounceError
	require.ErrorAs(t, err, &annErr)
	assert.Equal(t, AnnounceErrorCodeUnauthorized, annErr.AnnounceErrorCode())
}

func TestSession_AcceptAnnounce_PrefixOutsideRoots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := startAuthServer(t, ctx, roomTicketHandler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	cred, err := sess.Auth(ctx, []byte("room1"))
	require.NoError(t, err)
	defer func() { _ = cred.Close() }() // not actionable: the test is over

	// "/room/" holds room 2 as well, outside the ticket's root.
	ar, err := sess.AcceptAnnounce("/room/")
	if err == nil {
		actx, acancel := context.WithTimeout(ctx, 5*time.Second)
		defer acancel()
		_, err = ar.ReceiveAnnouncement(actx)
	}

	var annErr *AnnounceError
	require.ErrorAs(t, err, &annErr)
	assert.Equal(t, AnnounceErrorCodeUnauthorized, annErr.AnnounceErrorCode())
}

func TestSession_Announcements_PeerPublishesOnlyItsScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sessions := make(chan *Session, 1)
	addr := startAuthServer(t, ctx, roomTicketHandler, sessions)
	clientMux := NewTrackMux(0)
	// Announced before any grant allows it: hidden until one does.
	clientMux.PublishFunc(ctx, "/room/1/dave", authGroupHandler)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", clientMux)
	var server *Session
	select {
	case server = <-sessions:
	case <-ctx.Done():
		t.Fatal("no session")
	}
	ar, err := server.AcceptAnnounce("/room/")
	require.NoError(t, err)
	defer func() { _ = ar.Close() }() // not actionable: the test is over

	short, scancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer scancel()
	_, err = ar.ReceiveAnnouncement(short)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	cred, err := sess.Auth(ctx, []byte("publish"))
	require.NoError(t, err)
	defer func() { _ = cred.Close() }() // not actionable: the test is over
	// Announced once the grant allows it.
	clientMux.PublishFunc(ctx, "/room/1/carol", authGroupHandler)
	clientMux.PublishFunc(ctx, "/room/2/erin", authGroupHandler)

	actx, acancel := context.WithTimeout(ctx, 5*time.Second)
	defer acancel()
	var got []BroadcastPath
	for range 2 {
		ann, err := ar.ReceiveAnnouncement(actx)
		require.NoError(t, err)
		got = append(got, ann.BroadcastPath())
	}
	assert.ElementsMatch(t, []BroadcastPath{"/room/1/dave", "/room/1/carol"}, got,
		"the early broadcast is shown once the grant allows it")

	short2, scancel2 := context.WithTimeout(ctx, 200*time.Millisecond)
	defer scancel2()
	_, err = ar.ReceiveAnnouncement(short2)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the out-of-scope broadcast is not shown")
}

func TestCredential_Grant_Copy(t *testing.T) {
	cred := &Credential{grant: Grant{Root: "/room/1/", Subscribe: []string{"/"}}}

	g := cred.Grant()
	g.Subscribe[0] = "/host"

	assert.Equal(t, []string{"/"}, cred.Grant().Subscribe)
}

func TestCredential_Context_HandlerReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handler := AuthHandlerFunc(func(w *AuthWriter, r *AuthRequest) {
		if len(r.Token) == 0 {
			<-r.Context().Done()
			return
		}
		// The grant ends as the handler returns.
		_ = w.Grant(Grant{Root: "/room/1/", Subscribe: []string{"/"}})
	})
	addr := startAuthServer(t, ctx, handler, nil)
	sess := dialAuth(t, ctx, "moqt://"+addr+"/app", nil)
	cred, err := sess.Auth(ctx, []byte("room1"))
	require.NoError(t, err)
	defer func() { _ = cred.Close() }() // not actionable: the test is over

	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	select {
	case <-cred.Context().Done():
	case <-waitCtx.Done():
		t.Fatal("the credential outlived its handler")
	}
}

func TestReadAuthReply_Violation(t *testing.T) {
	tests := map[string]interface{ Encode(io.Writer) error }{
		"expires too late":    message.AuthOkMessage{Expires: maxAuthExpires + 1},
		"code over 32 bits":   message.AuthErrorMessage{Code: 1 << 32},
		"reason too long":     message.AuthErrorMessage{Reason: strings.Repeat("a", maxAuthReasonLength+1)},
		"reason is not UTF-8": message.AuthErrorMessage{Reason: string([]byte{0xff})},
	}

	for name, m := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, m.Encode(&buf))

			_, err := readAuthReply(&buf)

			assert.ErrorIs(t, err, errAuthViolation)
		})
	}
}

func TestReadAuthReply_LatestExpiry(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, message.AuthOkMessage{Expires: maxAuthExpires}.Encode(&buf))

	g, err := readAuthReply(&buf)

	require.NoError(t, err)
	assert.Equal(t, time.Duration(math.MaxInt64), g.MaxAge, "an expiry beyond a time.Duration is the longest one")
}

func TestReadAuthReply_ErrorLimits(t *testing.T) {
	tests := map[string]message.AuthErrorMessage{
		"largest code":   {Code: 1<<32 - 1},
		"longest reason": {Code: 0x2, Reason: strings.Repeat("a", maxAuthReasonLength)},
	}

	for name, m := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, m.Encode(&buf))

			_, err := readAuthReply(&buf)

			var authErr *AuthError
			require.ErrorAs(t, err, &authErr)
			assert.Equal(t, SessionErrorCode(m.Code), authErr.Code)
			assert.Equal(t, m.Reason, authErr.Reason)
		})
	}
}
