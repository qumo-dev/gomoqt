package webtransportgo

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingsEnableWebtransport is the HTTP/3 settings identifier for WebTransport,
// as defined in github.com/quic-go/webtransport-go/protocol.go.
const settingsEnableWebtransport uint64 = 0x2b603742

// Ensure init() configures the internal server (ConnContext must be non-nil).
func TestInit_ConnContextIsSet(t *testing.T) {
	srv := &Server{}
	srv.init()

	assert.NotNil(t, srv.internalServer.H3.ConnContext,
		"H3.ConnContext must be non-nil after ConfigureHTTP3Server; "+
			"a nil ConnContext causes every Upgrade() call to fail with "+
			"\"webtransport: missing QUIC connection\"")
}

// TestInit_EnableDatagramsIsSet verifies that H3.EnableDatagrams is true,
// which is required for HTTP/3-level QUIC datagram support used by WebTransport.
func TestInit_EnableDatagramsIsSet(t *testing.T) {
	srv := &Server{}
	srv.init()

	assert.True(t, srv.internalServer.H3.EnableDatagrams,
		"H3.EnableDatagrams must be true for WebTransport")
}

// TestInit_WebTransportSettingAdvertised verifies that the HTTP/3 SETTINGS
// frame will advertise WebTransport support to clients.
func TestInit_WebTransportSettingAdvertised(t *testing.T) {
	srv := &Server{}
	srv.init()

	require.NotNil(t, srv.internalServer.H3.AdditionalSettings,
		"H3.AdditionalSettings must not be nil")

	val, exists := srv.internalServer.H3.AdditionalSettings[settingsEnableWebtransport]
	assert.True(t, exists,
		"H3.AdditionalSettings must contain settingsEnableWebtransport (0x2b603742)")
	assert.Equal(t, uint64(1), val,
		"settingsEnableWebtransport must be set to 1")
}

// TestInit_CheckOriginPropagated verifies that the checkOrigin function
// provided by the caller is forwarded to the underlying webtransport-go Server.
func TestInit_CheckOriginPropagated(t *testing.T) {
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
func TestInit_NilCheckOriginDoesNotPanic(t *testing.T) {
	srv := &Server{}
	assert.NotPanics(t, func() {
		srv.init()
	})
}
