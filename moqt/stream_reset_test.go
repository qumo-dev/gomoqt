package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file run each case over native QUIC and over WebTransport:
// an error the peer ends a stream or the session with must reach moqt in the
// same shape over both.
var transportSchemes = map[string]struct {
	scheme string
}{
	"native QUIC":  {scheme: "moqt"},
	"WebTransport": {scheme: "https"},
}

// dialLoopback starts a loopback Server with mux and handler and returns a
// client session dialed to it over scheme. Both end with the test.
func dialLoopback(tb testing.TB, ctx context.Context, scheme string, mux *TrackMux, handler Handler) *Session {
	tb.Helper()
	addr := freePort(tb)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(tb)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    handler,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			tb.Logf("server: %v", err)
		}
	}()
	tb.Cleanup(func() { closeServer(tb, srv) })

	dialer := &Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // test-only self-signed cert
	var sess *Session
	require.Eventually(tb, func() bool {
		dialCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		var err error
		sess, err = dialer.Dial(dialCtx, scheme+"://"+addr+"/", nil)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	tb.Cleanup(func() { _ = sess.CloseWithError(NoError, "test done") }) // not actionable: cleanup
	return sess
}

// waitSession holds a session open until it ends.
func waitSession(sess *Session) { <-sess.Context().Done() }

// A subscription to a broadcast the publisher does not have ends in a
// *SubscribeError with NotFound: the code the publisher resets the stream
// with reaches the subscriber unchanged.
func TestSession_Subscribe_NotFound(t *testing.T) {
	for name, tt := range transportSchemes {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			sess := dialLoopback(t, ctx, tt.scheme, NewTrackMux(0), HandleFunc(waitSession))

			_, err := sess.Subscribe(ctx, "/missing", "video", nil)

			subErr, ok := errors.AsType[*SubscribeError](err)
			require.True(t, ok, "want a *SubscribeError, got %T: %v", err, err)
			assert.Equal(t, SubscribeErrorCodeNotFound, subErr.SubscribeErrorCode())
			assert.True(t, subErr.Remote)
		})
	}
}

// A subscriber that ends its subscription with a code ends the publisher's
// TrackWriter context with that code: Cause sees the application code, not
// the one WebTransport puts on the wire.
func TestTrackWriter_Context_SubscriberCloseCause(t *testing.T) {
	for name, tt := range transportSchemes {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			mux := NewTrackMux(0)
			writers := make(chan *TrackWriter, 1)
			mux.PublishFunc(ctx, "/present", func(tw *TrackWriter) {
				// The first group sends SUBSCRIBE_OK, which Subscribe waits for.
				if gw, err := tw.OpenGroup(tw.Context()); err == nil {
					_ = gw.Close() // not actionable: the subscriber need not read the group
				}
				writers <- tw
				<-tw.Context().Done()
			})
			sess := dialLoopback(t, ctx, tt.scheme, mux, HandleFunc(waitSession))
			tr, err := sess.Subscribe(ctx, "/present", "video", nil)
			require.NoError(t, err)
			var tw *TrackWriter
			select {
			case tw = <-writers:
			case <-ctx.Done():
				t.Fatal("the publisher did not get the subscription")
			}

			tr.CloseWithError(SubscribeErrorCodeTimeout)

			select {
			case <-tw.Context().Done():
			case <-ctx.Done():
				t.Fatal("the publisher's track did not end")
			}
			subErr, ok := errors.AsType[*SubscribeError](Cause(tw.Context()))
			require.True(t, ok, "want a *SubscribeError, got %v", Cause(tw.Context()))
			assert.Equal(t, SubscribeErrorCodeTimeout, subErr.SubscribeErrorCode())
			assert.True(t, subErr.Remote)
		})
	}
}

// A subscription opened after the peer closed the session ends in a
// *SessionError with the peer's code.
func TestSession_Subscribe_AfterPeerClose(t *testing.T) {
	for name, tt := range transportSchemes {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			handler := HandleFunc(func(sess *Session) {
				_ = sess.CloseWithError(UnauthorizedSessionErrorCode, "expired") // not actionable: the client observes the close
			})
			sess := dialLoopback(t, ctx, tt.scheme, NewTrackMux(0), handler)
			select {
			case <-sess.Context().Done():
			case <-ctx.Done():
				t.Fatal("the peer did not close the session")
			}

			_, err := sess.Subscribe(ctx, "/any", "video", nil)

			sessErr, ok := errors.AsType[*SessionError](err)
			require.True(t, ok, "want a *SessionError, got %T: %v", err, err)
			assert.Equal(t, UnauthorizedSessionErrorCode, sessErr.SessionErrorCode())
			assert.True(t, sessErr.Remote)
		})
	}
}
