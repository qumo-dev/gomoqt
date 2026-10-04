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
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebTransportSession_PeerCloseCause(t *testing.T) {
	addr := freePort(t)
	serverSessions := make(chan *Session, 2)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(t)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: HandleFunc(func(sess *Session) {
			serverSessions <- sess
			<-sess.Context().Done()
		}),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			t.Logf("server: %v", err)
		}
	}()
	t.Cleanup(func() { assert.NoError(t, srv.Close()) })

	dialer := &Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}
	dial := func() *Session {
		var client *Session
		require.Eventually(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			var err error
			client, err = dialer.Dial(ctx, "https://"+addr+"/", nil)
			return err == nil
		}, 5*time.Second, 50*time.Millisecond)
		return client
	}
	waitServer := func() *Session {
		select {
		case sess := <-serverSessions:
			return sess
		case <-time.After(5 * time.Second):
			t.Fatal("server did not accept WebTransport session")
			return nil
		}
	}
	assertPeerCause := func(ctx context.Context, reason string) {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("peer session did not close")
		}
		var appErr *transport.ApplicationError
		require.ErrorAs(t, context.Cause(ctx), &appErr)
		assert.Equal(t, transport.ApplicationErrorCode(2), appErr.ErrorCode)
		assert.Equal(t, reason, appErr.ErrorMessage)
		assert.True(t, appErr.Remote)
	}
	waitSetup := func(sess *Session) {
		select {
		case <-sess.peerSetupCh:
		case <-time.After(5 * time.Second):
			t.Fatal("peer SETUP did not arrive")
		}
	}

	client := dial()
	server := waitServer()
	waitSetup(client)
	waitSetup(server)
	require.NoError(t, server.CloseWithError(UnauthorizedSessionErrorCode, "expired"))
	assertPeerCause(client.Context(), "expired")

	client = dial()
	server = waitServer()
	waitSetup(client)
	waitSetup(server)
	require.NoError(t, client.CloseWithError(UnauthorizedSessionErrorCode, "refused"))
	assertPeerCause(server.Context(), "refused")
}
