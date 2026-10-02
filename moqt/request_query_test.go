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

// TestDial_NativeQUIC_QueryReachesServer dials a real native-QUIC server with a
// query and checks the server's handler sees it as RequestQuery, apart from
// RequestPath, as a WebTransport handler would.
func TestDial_NativeQUIC_QueryReachesServer(t *testing.T) {
	addr := freePort(t)
	type request struct{ path, query string }
	seen := make(chan request, 1)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(t)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			seen <- request{sess.RequestPath(), sess.RequestQuery()}
			<-sess.Context().Done()
		}),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			t.Logf("server: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Close() })

	var sess *Session
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		var err error
		sess, err = (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+addr+"/live/alice?jwt=a.b.c", nil)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "server never accepted the session")
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	select {
	case got := <-seen:
		assert.Equal(t, request{path: "/live/alice", query: "jwt=a.b.c"}, got)
	case <-time.After(5 * time.Second):
		t.Fatal("the server handler never ran")
	}
	assert.Equal(t, "jwt=a.b.c", sess.RequestQuery(), "the client reports the query it dialed")
}
