package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectTo serves every subscription with a redirect to to, as an
// application's handler on a TrackMux does, and refuses a subscriber that
// does not follow redirects.
func redirectTo(to Redirect) func(*TrackWriter) {
	return func(tw *TrackWriter) {
		if err := tw.Redirect(tw.Context(), to); err != nil {
			tw.CloseWithError(SubscribeErrorCodeNotFound)
		}
	}
}

// publishEcho serves one group whose only frame names the track and the
// priority the subscriber asked for, so a test can tell which track it
// reached and that its subscription parameters came along.
func publishEcho(tw *TrackWriter) {
	gw, err := tw.OpenGroup(tw.Context())
	if err != nil {
		return
	}
	frame := NewFrame(32)
	frame.Write(fmt.Appendf(nil, "%s:%d", tw.TrackName, tw.TrackConfig().Priority))
	_ = gw.WriteFrame(frame) // not actionable: the subscriber reports what it read
	_ = gw.Close()           // not actionable: as above
	<-tw.Context().Done()
}

// elsewhereRedirect is the redirect to another session the test muxes serve.
var elsewhereRedirect = Redirect{URI: "moqt://relay-b.example/", BroadcastPath: "/real", TrackName: "video"}

// newRedirectMux publishes /real and the redirects around it that the tests
// below subscribe to.
func newRedirectMux(ctx context.Context) *TrackMux {
	mux := NewTrackMux(0)
	mux.PublishFunc(ctx, "/real", publishEcho)
	mux.PublishFunc(ctx, "/alias", redirectTo(Redirect{BroadcastPath: "/real"}))
	mux.PublishFunc(ctx, "/alias-of-alias", redirectTo(Redirect{BroadcastPath: "/alias"}))
	mux.PublishFunc(ctx, "/renamed", redirectTo(Redirect{BroadcastPath: "/real", TrackName: "audio"}))
	mux.PublishFunc(ctx, "/loop-a", redirectTo(Redirect{BroadcastPath: "/loop-b"}))
	mux.PublishFunc(ctx, "/loop-b", redirectTo(Redirect{BroadcastPath: "/loop-a"}))
	mux.PublishFunc(ctx, "/elsewhere", redirectTo(elsewhereRedirect))
	return mux
}

// startRedirectServer serves mux on a loopback address for native QUIC and
// WebTransport alike, and hands each accepted session to onSession.
func startRedirectServer(tb testing.TB, mux *TrackMux, onSession func(*Session)) string {
	tb.Helper()
	addr := freePort(tb)
	server := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(tb)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			if onSession != nil {
				onSession(sess)
			}
			<-sess.Context().Done()
		}),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server: %v", err)
		}
	}()
	tb.Cleanup(func() { closeServer(tb, server) })
	return addr
}

// dialRedirectServer dials addr over scheme, retrying until the listener is
// up, with config and a mux the client publishes from.
func dialRedirectServer(tb testing.TB, ctx context.Context, scheme, addr string, checkRedirect CheckRedirectFunc, mux *TrackMux) *Session {
	tb.Helper()
	dialer := Dialer{
		CheckRedirect: checkRedirect,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only self-signed cert
	}
	var sess *Session
	require.Eventually(tb, func() bool {
		s, err := dialer.Dial(ctx, scheme+"://"+addr+"/", mux)
		if err != nil {
			return false
		}
		sess = s
		return true
	}, 5*time.Second, 50*time.Millisecond, "dial the test server over %s", scheme)
	tb.Cleanup(func() { _ = sess.CloseWithError(NoError, "") }) // not actionable: cleanup
	return sess
}

// readEcho reads the frame publishEcho wrote.
func readEcho(tb testing.TB, ctx context.Context, tr *TrackReader) string {
	tb.Helper()
	gr, err := tr.AcceptGroup(ctx)
	require.NoError(tb, err)
	frame := NewFrame(32)
	require.NoError(tb, gr.ReadFrame(frame))
	return string(frame.Body())
}

// TestSession_Subscribe_Redirect checks what Session.Subscribe returns for each
// kind of redirect, over both transports a client dials.
func TestSession_Subscribe_Redirect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	addr := startRedirectServer(t, newRedirectMux(ctx), nil)

	type subscribeCase struct {
		path     BroadcastPath
		wantPath BroadcastPath
		wantName TrackName
		wantErr  error
		wantTo   *Redirect
		notFound bool
	}
	tests := map[string]subscribeCase{
		"no redirect":       {path: "/real", wantPath: "/real", wantName: "video"},
		"one redirect":      {path: "/alias", wantPath: "/real", wantName: "video"},
		"two redirects":     {path: "/alias-of-alias", wantPath: "/real", wantName: "video"},
		"another track":     {path: "/renamed", wantPath: "/real", wantName: "audio"},
		"loop":              {path: "/loop-a", wantErr: ErrTooManyRedirects, wantTo: &Redirect{BroadcastPath: "/loop-b", TrackName: "video"}},
		"another session":   {path: "/elsewhere", wantTo: &elsewhereRedirect},
		"no such broadcast": {path: "/missing", notFound: true},
	}

	for _, scheme := range []string{"moqt", "https"} {
		t.Run(scheme, func(t *testing.T) {
			sess := dialRedirectServer(t, ctx, scheme, addr, nil, nil)

			for name, tt := range tests {
				t.Run(name, func(t *testing.T) {
					tr, err := sess.Subscribe(ctx, tt.path, "video", &SubscribeConfig{Priority: 3})

					switch {
					case tt.wantTo != nil:
						if tt.wantErr != nil {
							assert.ErrorIs(t, err, tt.wantErr)
						}
						redirect, ok := errors.AsType[*RedirectError](err)
						require.True(t, ok, "want a *RedirectError, got %v", err)
						assert.Equal(t, *tt.wantTo, redirect.Redirect)
						assert.Nil(t, tr)
						return
					case tt.notFound:
						subErr, ok := errors.AsType[*SubscribeError](err)
						require.True(t, ok, "want a *SubscribeError, got %v", err)
						assert.Equal(t, SubscribeErrorCodeNotFound, subErr.SubscribeErrorCode())
						return
					}
					require.NoError(t, err)
					defer tr.Close()
					assert.Equal(t, tt.wantPath, tr.BroadcastPath)
					assert.Equal(t, tt.wantName, tr.TrackName)
					// The priority asked for reached the track redirected to.
					assert.Equal(t, fmt.Sprintf("%s:3", tt.wantName), readEcho(t, ctx, tr))
				})
			}

			// Following redirects leaves no subscription behind but the ones
			// that were returned, all closed above.
			assert.Eventually(t, func() bool {
				sess.trackReaderMapLocker.Lock()
				defer sess.trackReaderMapLocker.Unlock()
				return len(sess.trackReaders) == 0
			}, time.Second, 10*time.Millisecond, "redirected subscriptions were cleaned up")
		})
	}
}

// TestSession_Subscribe_Redirect_CheckRedirect checks that Dialer.CheckRedirect
// sees each redirect with the ones followed before it, and what its answers do.
func TestSession_Subscribe_Redirect_CheckRedirect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	addr := startRedirectServer(t, newRedirectMux(ctx), nil)

	type call struct {
		to  BroadcastPath
		via []BroadcastPath
	}
	var calls []call
	errVeto := errors.New("vetoed")
	sess := dialRedirectServer(t, ctx, "moqt", addr, func(to Redirect, via []Redirect) error {
		c := call{to: to.BroadcastPath}
		for _, v := range via {
			c.via = append(c.via, v.BroadcastPath)
		}
		calls = append(calls, c)
		// Mutating via must not change the history of later calls.
		for i := range via {
			via[i].BroadcastPath = "/tampered"
		}
		switch {
		case to.BroadcastPath == "/loop-b":
			return ErrStopRedirect
		case to.BroadcastPath == "/real" && len(via) > 0:
			return errVeto
		}
		return nil
	}, nil)

	tests := map[string]struct {
		path      BroadcastPath
		wantPath  BroadcastPath
		wantErr   error
		wantTo    BroadcastPath
		wantCalls []call
	}{
		"follows": {
			path:      "/alias",
			wantPath:  "/real",
			wantCalls: []call{{to: "/real"}},
		},
		"vetoes the second": {
			path:      "/alias-of-alias",
			wantErr:   errVeto,
			wantTo:    "/real",
			wantCalls: []call{{to: "/alias"}, {to: "/real", via: []BroadcastPath{"/alias"}}},
		},
		"stops": {
			path:      "/loop-a",
			wantTo:    "/loop-b",
			wantCalls: []call{{to: "/loop-b"}},
		},
		"not asked about another session": {
			path:      "/elsewhere",
			wantTo:    "/real",
			wantCalls: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			calls = nil

			tr, err := sess.Subscribe(ctx, tt.path, "video", nil)

			assert.Equal(t, tt.wantCalls, calls)
			if tt.wantTo == "" {
				require.NoError(t, err)
				defer tr.Close()
				assert.Equal(t, tt.wantPath, tr.BroadcastPath)
				return
			}
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.NotErrorIs(t, err, ErrStopRedirect, "a stopped redirect is returned as itself")
			redirect, ok := errors.AsType[*RedirectError](err)
			require.True(t, ok, "want a *RedirectError, got %v", err)
			assert.Equal(t, tt.wantTo, redirect.Redirect.BroadcastPath)
		})
	}
}

// TestSession_Subscribe_Redirect_ServerToClient runs the other direction: a
// server subscribes to what a client publishes, and the client redirects.
func TestSession_Subscribe_Redirect_ServerToClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	var accepted atomic.Pointer[Session]
	addr := startRedirectServer(t, NewTrackMux(0), func(sess *Session) { accepted.Store(sess) })

	clientErrs := make(chan error, 1)
	clientMux := NewTrackMux(0)
	clientMux.PublishFunc(ctx, "/real", publishEcho)
	clientMux.PublishFunc(ctx, "/alias", redirectTo(Redirect{BroadcastPath: "/real"}))
	clientMux.PublishFunc(ctx, "/elsewhere", func(tw *TrackWriter) {
		err := tw.Redirect(tw.Context(), elsewhereRedirect)
		clientErrs <- err
		tw.CloseWithError(SubscribeErrorCodeNotFound)
	})
	dialRedirectServer(t, ctx, "moqt", addr, nil, clientMux)

	require.Eventually(t, func() bool { return accepted.Load() != nil }, 5*time.Second, 10*time.Millisecond)
	server := accepted.Load()

	t.Run("within the session", func(t *testing.T) {
		tr, err := server.Subscribe(ctx, "/alias", "video", nil)
		require.NoError(t, err)
		defer tr.Close()
		assert.Equal(t, BroadcastPath("/real"), tr.BroadcastPath)
		assert.Equal(t, "video:0", readEcho(t, ctx, tr))
	})

	t.Run("a client may not send another URI", func(t *testing.T) {
		_, err := server.Subscribe(ctx, "/elsewhere", "video", nil)

		subErr, ok := errors.AsType[*SubscribeError](err)
		require.True(t, ok, "want a *SubscribeError, got %v", err)
		assert.Equal(t, SubscribeErrorCodeNotFound, subErr.SubscribeErrorCode())
		select {
		case clientErr := <-clientErrs:
			assert.ErrorIs(t, clientErr, ErrClientRedirectURI)
		case <-ctx.Done():
			t.Fatal("the client's handler did not run")
		}
		assert.NoError(t, server.Context().Err(), "the session stays up")
	})
}
