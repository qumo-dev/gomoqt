package webtransportgo

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestInit_CheckOriginPropagated verifies that the checkOrigin function
// provided by the caller is forwarded to the underlying webtransport-go Server.
func TestServer_Init_CheckOriginPropagated(t *testing.T) {
	called := false
	checkOrigin := func(r *http.Request) bool {
		called = true
		return true
	}

	srv := &Server{CheckOrigin: checkOrigin}
	srv.init()

	require.NotNil(t, srv.internalServer.CheckOrigin,
		"CheckOrigin must be propagated to the underlying server")

	// Invoke the stored function to confirm it's the one we passed in.
	srv.internalServer.CheckOrigin(&http.Request{})
	assert.True(t, called, "stored CheckOrigin should be our function")
}

// TestInit_NilCheckOriginDoesNotPanic verifies that passing nil for
// CheckOrigin is safe (the underlying library substitutes its own default).
func TestServer_Init_NilCheckOriginDoesNotPanic(t *testing.T) {
	srv := &Server{}
	assert.NotPanics(t, func() {
		srv.init()
	})
}
