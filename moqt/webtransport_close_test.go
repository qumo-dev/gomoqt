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
		var sessErr *SessionError
		require.ErrorAs(t, Cause(ctx), &sessErr)
		assert.Equal(t, UnauthorizedSessionErrorCode, sessErr.SessionErrorCode())
		assert.Equal(t, reason, sessErr.ErrorMessage)
		assert.True(t, sessErr.Remote)
	}

	client := dial()
	server := waitServer()
	require.NoError(t, server.CloseWithError(UnauthorizedSessionErrorCode, "expired"))
	assertPeerCause(client.Context(), "expired")

	client = dial()
	server = waitServer()
	require.NoError(t, client.CloseWithError(UnauthorizedSessionErrorCode, "refused"))
	assertPeerCause(server.Context(), "refused")
}

type connKickKey struct{}

// Over WebTransport, cancelling the context Server.ConnContext returned ends
// the session's context with its cause, as it does over native QUIC, although
// the session's values come from the upgrade request.
func TestWebTransportSession_ConnContextCancelEndsSession(t *testing.T) {
	addr := freePort(t)
	serverSessions := make(chan *Session, 1)
	srv := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(t)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ConnContext: func(ctx context.Context, _ StreamConn) context.Context {
			ctx, kick := context.WithCancelCause(ctx)
			return context.WithValue(ctx, connKickKey{}, kick)
		},
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
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		client, err := dialer.Dial(ctx, "https://"+addr+"/", nil)
		if err == nil {
			t.Cleanup(func() { _ = client.CloseWithError(NoError, "") })
		}
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	var server *Session
	select {
	case server = <-serverSessions:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not accept WebTransport session")
	}
	kick, ok := server.Context().Value(connKickKey{}).(context.CancelCauseFunc)
	require.True(t, ok, "the session carries ConnContext's values")
	appErr := errors.New("application ended the connection")

	kick(appErr)

	select {
	case <-server.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling ConnContext's context did not end the session")
	}
	assert.ErrorIs(t, context.Cause(server.Context()), appErr)
}
