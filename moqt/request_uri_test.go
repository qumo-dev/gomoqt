package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDial_NativeQUIC_QueryReachesServer dials a real native-QUIC server with a
// query and checks the server's handler sees it in RequestURI, after "?", as
// a WebTransport handler would, and can split it with url.Parse.
func TestDial_NativeQUIC_QueryReachesServer(t *testing.T) {
	addr := freePort(t)
	seen := make(chan string, 1)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(t)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			seen <- sess.RequestURI()
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
		sess, err = (&Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}).Dial(ctx, "moqt://"+addr+"/live/alice%20smith?jwt=a.b.c", nil)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "server never accepted the session")
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	select {
	case got := <-seen:
		assert.Equal(t, "/live/alice%20smith?jwt=a.b.c", got, "escaped as dialed, query included")
		u, err := url.Parse(got)
		require.NoError(t, err)
		assert.Equal(t, "/live/alice smith", u.Path)
		assert.Equal(t, "a.b.c", u.Query().Get("jwt"))
	case <-time.After(5 * time.Second):
		t.Fatal("the server handler never ran")
	}
	assert.Equal(t, "/live/alice%20smith?jwt=a.b.c", sess.RequestURI(), "the client reports what the server sees")
}
