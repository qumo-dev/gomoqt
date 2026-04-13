package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialer_Dial_HTTPSRoutesToDialWebTransport(t *testing.T) {
	called := false
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 50 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			called = true
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 250*time.Millisecond)
			assert.Equal(t, "https://example.com:443/session", addr)
			assert.Nil(t, header)
			assert.Nil(t, tlsConfig)

			conn := &FakeWebTransportSession{}
			conn.AcceptStreamFunc = func(context.Context) (transport.Stream, error) { return nil, context.Canceled }
			conn.AcceptUniStreamFunc = func(context.Context) (transport.ReceiveStream, error) { return nil, context.Canceled }
			conn.LocalAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443} }
			conn.RemoteAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443} }

			return &http.Response{StatusCode: http.StatusOK}, conn, nil
		},
	}

	sess, err := dialer.Dial(context.Background(), "https://example.com:443/session", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.NotNil(t, sess.conn)
	assert.True(t, called)

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_Dial_MOQTRoutesToDialQUIC(t *testing.T) {
	called := false
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 50 * time.Millisecond},
		DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
			called = true
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 250*time.Millisecond)
			assert.Equal(t, "example.com:9000", addr)
			require.NotNil(t, tlsConfig)
			assert.Equal(t, []string{NextProtoMOQ}, tlsConfig.NextProtos)
			assert.Nil(t, quicConfig)

			conn := &FakeStreamConn{}
			return conn, nil
		},
	}

	sess, err := dialer.Dial(context.Background(), "moqt://example.com:9000", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.True(t, called)

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_Dial_InvalidScheme(t *testing.T) {
	dialer := &Dialer{}

	sess, err := dialer.Dial(context.Background(), "ftp://example.com", nil)
	require.Error(t, err)
	assert.Nil(t, sess)
	assert.ErrorIs(t, err, ErrInvalidScheme)
}

func TestDialer_DialWebTransport_DefaultPath(t *testing.T) {
	recordedTarget := ""
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 25 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			recordedTarget = addr
			conn := &FakeWebTransportSession{}
			conn.AcceptStreamFunc = func(context.Context) (transport.Stream, error) { return nil, context.Canceled }
			conn.AcceptUniStreamFunc = func(context.Context) (transport.ReceiveStream, error) { return nil, context.Canceled }
			conn.LocalAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443} }
			conn.RemoteAddrFunc = func() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443} }
			return &http.Response{StatusCode: http.StatusOK}, conn, nil
		},
	}

	sess, err := dialer.DialWebTransport(context.Background(), "example.com:8443", "", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "https://example.com:8443/", recordedTarget)

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_DialQUIC_DefaultTLSConfig(t *testing.T) {
	recordedTLS := (*tls.Config)(nil)
	recordedDeadline := time.Time{}
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 25 * time.Millisecond},
		DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
			recordedTLS = tlsConfig
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			recordedDeadline = deadline
			assert.Equal(t, "example.com:9000", addr)
			assert.Nil(t, quicConfig)
			assert.Equal(t, []string{NextProtoMOQ}, tlsConfig.NextProtos)

			conn := &FakeStreamConn{}
			return conn, nil
		},
	}

	sess, err := dialer.DialQUIC(context.Background(), "example.com:9000", "/session", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.NotNil(t, recordedTLS)
	assert.False(t, recordedDeadline.IsZero())

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_DialWebTransport_CustomDialError(t *testing.T) {
	dialErr := errors.New("dial failed")
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 25 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			return nil, nil, dialErr
		},
	}

	sess, err := dialer.DialWebTransport(context.Background(), "example.com:8443", "/session", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, dialErr)
	assert.Nil(t, sess)
}
