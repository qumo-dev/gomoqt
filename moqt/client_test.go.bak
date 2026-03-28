package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestClient_InitAndTimeout(t *testing.T) {
	c := &Client{}
	c.init()
	c.init()

	require.NotNil(t, c.activeSess)
	require.NotNil(t, c.doneChan)
	assert.Equal(t, 5*time.Second, c.Config.setupTimeout())

	c.Config = &Config{SetupTimeout: 42 * time.Second}
	assert.Equal(t, 42*time.Second, c.Config.setupTimeout())
}

func TestClient_AddRemoveSession(t *testing.T) {
	c := &Client{}
	c.init()

	c.addSession(nil)
	assert.Len(t, c.activeSess, 0)

	s := &Session{}
	c.addSession(s)
	assert.Len(t, c.activeSess, 1)

	c.removeSession(nil)
	assert.Len(t, c.activeSess, 1)

	c.removeSession(s)
	assert.Len(t, c.activeSess, 0)
}

func TestClient_Dial_InvalidInputsAndShutdown(t *testing.T) {
	c := &Client{}

	_, err := c.Dial(context.Background(), "ftp://example.com", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidScheme)

	_, err = c.Dial(context.Background(), "://", nil)
	require.Error(t, err)

	c.inShutdown.Store(true)
	_, err = c.Dial(context.Background(), "https://example.com", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestClient_DialWebTransport_And_DialQUIC_Shutdown(t *testing.T) {
	c := &Client{}
	c.inShutdown.Store(true)

	_, err := c.DialWebTransport(context.Background(), "example.com:443", "/", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)

	_, err = c.DialQUIC(context.Background(), "example.com:443", "/", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestClient_DialWebTransport_CustomDialError(t *testing.T) {
	c := &Client{}
	wantErr := errors.New("webtransport dial failed")
	c.DialWebTransportFunc = func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, StreamConn, error) {
		return nil, nil, wantErr
	}

	_, err := c.DialWebTransport(context.Background(), "example.com:443", "/test", NewTrackMux())
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestClient_DialQUIC_CustomDialError(t *testing.T) {
	c := &Client{}
	wantErr := errors.New("quic dial failed")
	c.DialQUICFunc = func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
		return nil, wantErr
	}

	_, err := c.DialQUIC(context.Background(), "example.com:443", "/test", NewTrackMux())
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestClient_DialQUIC_DefaultALPN(t *testing.T) {
	c := &Client{TLSConfig: &tls.Config{}}
	wantErr := errors.New("stop")
	var captured []string

	c.DialQUICFunc = func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
		captured = append([]string(nil), tlsConfig.NextProtos...)
		return nil, wantErr
	}

	_, err := c.DialQUIC(context.Background(), "example.com:443", "/test", NewTrackMux())
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Equal(t, []string{NextProtoMOQ}, captured)
}

func TestClient_Close_NoSessions(t *testing.T) {
	c := &Client{}
	c.init()

	err := c.Close()
	require.NoError(t, err)
	assert.True(t, c.shuttingDown())
}

func TestClient_Close_WithSession(t *testing.T) {
	c := &Client{}
	c.init()

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	var sess *Session
	sess = newSession(conn, NewTrackMux(), func() { c.removeSession(sess) })
	c.addSession(sess)

	done := make(chan error, 1)
	go func() { done <- c.Close() }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Close timed out")
	}
}

func TestClient_Shutdown_ContextCanceledWithActiveSession(t *testing.T) {
	c := &Client{}
	c.init()

	conn := &MockStreamConn{}
	conn.On("Context").Return(context.Background())
	conn.On("AcceptStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("AcceptUniStream", mock.Anything).Return(nil, context.Canceled)
	conn.On("CloseWithError", mock.Anything, mock.Anything).Return(nil)

	sess := newSession(conn, NewTrackMux(), nil)
	c.addSession(sess)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Shutdown(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	assert.Len(t, id1, 8)
	assert.Len(t, id2, 8)
	assert.NotEqual(t, id1, id2)
}
