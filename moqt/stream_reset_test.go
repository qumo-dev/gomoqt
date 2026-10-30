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

// transportSchemes maps each transport a client dials to its URL scheme: a
// stream reset must reach moqt in the same shape over both.
var transportSchemes = map[string]string{
	"native QUIC":  "moqt",
	"WebTransport": "https",
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
	for name, scheme := range transportSchemes {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			sess := dialLoopback(t, ctx, scheme, NewTrackMux(0), HandleFunc(waitSession))

			_, err := sess.Subscribe(ctx, "/missing", "video", nil)

			subErr, ok := errors.AsType[*SubscribeError](err)
			require.True(t, ok, "want a *SubscribeError, got %T: %v", err, err)
			assert.Equal(t, SubscribeErrorCodeNotFound, subErr.SubscribeErrorCode())
			assert.True(t, subErr.Remote)
		})
	}
}
