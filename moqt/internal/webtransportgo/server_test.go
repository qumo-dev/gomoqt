package webtransportgo

import (
	"testing"

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
