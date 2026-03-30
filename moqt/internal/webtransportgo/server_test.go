package webtransportgo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestServer_Init_CreatesInternalServer verifies wrapper init allocates the
// upstream server and an HTTP/3 server container.
func TestServer_Init_CreatesInternalServer(t *testing.T) {
	srv := &Server{}
	srv.init()

	require.NotNil(t, srv.internalServer)
	require.NotNil(t, srv.internalServer.H3)
}

// TestInit_DoesNotPanic verifies that init remains safe with default configuration.
func TestServer_Init_DoesNotPanic(t *testing.T) {
	srv := &Server{}
	require.NotPanics(t, func() {
		srv.init()
	})
}

func TestServer_ServeQUICConn_InvalidConnType(t *testing.T) {
	srv := &Server{}
	err := srv.ServeQUICConn(&MockStreamConn{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid connection type")
}

func TestServer_ServeQUICConn_NilConn(t *testing.T) {
	srv := &Server{}
	require.NoError(t, srv.ServeQUICConn(nil))
}

func TestServer_Shutdown_WithCancelledContext(t *testing.T) {
	srv := &Server{}
	srv.init()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := srv.Shutdown(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestServer_Shutdown_Completes(t *testing.T) {
	srv := &Server{}
	srv.init()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	require.NoError(t, srv.Shutdown(ctx))
}
