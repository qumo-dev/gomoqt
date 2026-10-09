package webtransportgo

import (
	"context"
	"net/http"
	"testing"
	"time"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	quicgo_quicgo "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
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

// TestInit_DoesNotPanic verifies that init remains safe with default configuration.
func TestServer_Init_DoesNotPanic(t *testing.T) {
	srv := &Server{}
	require.NotPanics(t, func() {
		srv.init()
	})
}

func TestServer_ServeQUICConn_InvalidConnType(t *testing.T) {
	srv := &Server{}
	err := srv.ServeQUICConn(&FakeStreamConn{})
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

func TestServer_Init_WithCustomHandler(t *testing.T) {
	mux := http.NewServeMux()
	srv := &Server{Handler: mux}
	srv.init()

	require.NotNil(t, srv.internalServer)
	require.NotNil(t, srv.internalServer.H3)
	assert.Equal(t, mux, srv.internalServer.H3.Handler)
}

func TestServer_Init_WithPresetInternalServer(t *testing.T) {
	h3 := &http3.Server{}
	internal := &quicgo_webtransportgo.Server{H3: h3}
	cfg := &Config{MaxIncomingStreams: 42}
	srv := &Server{internalServer: internal, Config: cfg}
	srv.init()

	// Should use the pre-set internalServer and propagate Config
	assert.Same(t, internal, srv.internalServer)
	assert.Same(t, h3, srv.internalServer.H3)
	assert.Same(t, cfg, srv.internalServer.Config)
	// ConnContext should be wired
	assert.NotNil(t, srv.internalServer.H3.ConnContext)
}

func TestServer_Init_WithPresetInternalServerNilH3(t *testing.T) {
	internal := &quicgo_webtransportgo.Server{H3: nil}
	mux := http.NewServeMux()
	srv := &Server{internalServer: internal, Handler: mux}
	srv.init()

	// H3 should be created with the Handler
	assert.NotNil(t, srv.internalServer.H3)
	assert.Equal(t, mux, srv.internalServer.H3.Handler)
}

func TestServer_Init_Idempotent(t *testing.T) {
	srv := &Server{}
	srv.init()
	first := srv.internalServer
	srv.init()
	assert.Same(t, first, srv.internalServer)
}

func TestServer_Close_NilInternalServer(t *testing.T) {
	srv := &Server{}
	// Don't call init — internalServer is nil
	err := srv.Close()
	assert.NoError(t, err)
}

func TestServer_Close_AfterInit(t *testing.T) {
	srv := &Server{}
	srv.init()

	err := srv.Close()
	assert.NoError(t, err)
}

func TestServer_Shutdown_NilInternalServer(t *testing.T) {
	srv := &Server{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestServer_Serve_NilPacketConn(t *testing.T) {
	srv := &Server{}
	err := srv.Serve(nil)
	assert.Error(t, err)
}

func TestServer_Init_ConnContext_WithStoredContext(t *testing.T) {
	srv := &Server{}
	srv.init()

	storedCtx := context.WithValue(context.Background(), struct{ key string }{"test"}, "value")
	var conn quicgo_quicgo.Conn
	srv.connContexts.Store(&conn, storedCtx)
	defer srv.connContexts.Delete(&conn)

	result := srv.internalServer.H3.ConnContext(context.Background(), &conn)
	assert.Equal(t, storedCtx, result)
}

func TestServer_Init_ConnContext_WithoutStoredContext(t *testing.T) {
	srv := &Server{}
	srv.init()

	fallbackCtx := context.WithValue(context.Background(), struct{ key string }{"fallback"}, "yes")
	var conn quicgo_quicgo.Conn

	result := srv.internalServer.H3.ConnContext(fallbackCtx, &conn)
	assert.Equal(t, fallbackCtx, result)
}

// FakeQUICConnProvider implements both transport.StreamConn and quicConnProvider
// to test the ServeQUICConn happy path.
type FakeQUICConnProvider struct {
	FakeStreamConn
	qc *quicgo_quicgo.Conn
}

func (f *FakeQUICConnProvider) QUICConn() *quicgo_quicgo.Conn {
	return f.qc
}

func TestServer_ServeQUICConn_QUICConnProviderBranch(t *testing.T) {
	// We cannot create a valid *quic.Conn without a real QUIC connection,
	// so we verify the quicConnProvider dispatch indirectly:
	// FakeQUICConnProvider satisfies the interface and we confirm the
	// error message differs from the "invalid connection type" path.
	srv := &Server{}

	provider := &FakeQUICConnProvider{
		qc: nil, // nil QUICConn — will panic in upstream, caught below
	}

	assert.Panics(t, func() {
		_ = srv.ServeQUICConn(provider)
	})
}
