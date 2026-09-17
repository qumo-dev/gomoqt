package moqt

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

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
			conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
			conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
			conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
			conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}

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

func TestDialer_Dial_WebTransportDefaultPath(t *testing.T) {
	recordedTarget := ""
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 25 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			recordedTarget = addr
			conn := &FakeWebTransportSession{}
			conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
			conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
			conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
			conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
			return &http.Response{StatusCode: http.StatusOK}, conn, nil
		},
	}

	sess, err := dialer.Dial(context.Background(), "https://example.com:8443", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "https://example.com:8443/", recordedTarget)

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_Dial_QUICDefaultTLSConfig(t *testing.T) {
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

	sess, err := dialer.Dial(context.Background(), "moqt://example.com:9000/", nil)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.NotNil(t, recordedTLS)
	assert.False(t, recordedDeadline.IsZero())

	t.Cleanup(func() {
		_ = sess.CloseWithError(NoError, "")
	})
}

func TestDialer_Dial_WebTransportCustomDialError(t *testing.T) {
	dialErr := errors.New("dial failed")
	dialer := &Dialer{
		Config: &Config{SetupTimeout: 25 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			return nil, nil, dialErr
		},
	}

	sess, err := dialer.Dial(context.Background(), "https://example.com:8443/session", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, dialErr)
	assert.Nil(t, sess)
}

// TestDialer_Dial_PopulatesSessionPath verifies Session.Path reports the path
// that was actually dialed, on both bindings.
func TestDialer_Dial_PopulatesSessionPath(t *testing.T) {
	newWebTransportDialer := func(recordTarget *string) *Dialer {
		return &Dialer{
			Config: &Config{SetupTimeout: 50 * time.Millisecond},
			DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
				*recordTarget = addr
				conn := &FakeWebTransportSession{}
				conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
				conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
				conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
				conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
				return &http.Response{StatusCode: http.StatusOK}, conn, nil
			},
		}
	}

	t.Run("WebTransport", func(t *testing.T) {
		var target string
		sess, err := newWebTransportDialer(&target).Dial(context.Background(), "https://example.com:443/live/alice", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

		assert.Equal(t, "https://example.com:443/live/alice", target)
		assert.Equal(t, "/live/alice", sess.Path())
	})

	t.Run("WebTransportNoPathDefaultsToRoot", func(t *testing.T) {
		var target string
		sess, err := newWebTransportDialer(&target).Dial(context.Background(), "https://example.com:443", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

		assert.Equal(t, "https://example.com:443/", target)
		assert.Equal(t, "/", sess.Path())
	})

	t.Run("NativeQUIC", func(t *testing.T) {
		d := &Dialer{
			Config: &Config{SetupTimeout: 50 * time.Millisecond},
			DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
				conn := &FakeStreamConn{}
				conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
				conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
				return conn, nil
			},
		}

		sess, err := d.Dial(context.Background(), "moqt://example.com:9000/live/alice", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

		assert.Equal(t, "/live/alice", sess.Path())
	})

	t.Run("NativeQUICNoPathDefaultsToRoot", func(t *testing.T) {
		d := &Dialer{
			Config: &Config{SetupTimeout: 50 * time.Millisecond},
			DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
				conn := &FakeStreamConn{}
				conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
				conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
				return conn, nil
			},
		}

		sess, err := d.Dial(context.Background(), "moqt://example.com:9000", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

		assert.Equal(t, "/", sess.Path())
	})
}

// TestDialer_Dial_PreservesQuery verifies the query survives into the dialed
// WebTransport request URI. Dial previously rebuilt the target as
// "https://" + host + url.Path, which discarded it.
func TestDialer_Dial_PreservesQuery(t *testing.T) {
	var target string
	d := &Dialer{
		Config: &Config{SetupTimeout: 50 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			target = addr
			conn := &FakeWebTransportSession{}
			conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
			conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
			conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
			conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
			return &http.Response{StatusCode: http.StatusOK}, conn, nil
		},
	}

	sess, err := d.Dial(context.Background(), "https://example.com:443/session?token=abc&hub=east", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	assert.Equal(t, "https://example.com:443/session?token=abc&hub=east", target)
	// Session.Path is the path alone; the query reaches the server's
	// http.Handler as part of the request URI.
	assert.Equal(t, "/session", sess.Path())
}

// TestDialer_Dial_StripsFragment verifies a fragment is not sent to the server.
// A fragment is client-side only and has no place in a request URI.
func TestDialer_Dial_StripsFragment(t *testing.T) {
	var target string
	d := &Dialer{
		Config: &Config{SetupTimeout: 50 * time.Millisecond},
		DialWebTransportFunc: func(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, WebTransportSession, error) {
			target = addr
			conn := &FakeWebTransportSession{}
			conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
			conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
			conn.LocalAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
			conn.RemoteAddrValue = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}
			return &http.Response{StatusCode: http.StatusOK}, conn, nil
		},
	}

	sess, err := d.Dial(context.Background(), "https://example.com:443/session#frag", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	assert.Equal(t, "https://example.com:443/session", target)
	assert.Equal(t, "/session", sess.Path())
}

// TestDialer_Dial_QUICRejectsQuery verifies a moqt URL carrying a query is
// refused rather than dialed without it. The native QUIC binding conveys only a
// path (the SETUP Path parameter), so honoring such a URL is impossible and
// dropping the query would silently connect to a different endpoint.
func TestDialer_Dial_QUICRejectsQuery(t *testing.T) {
	dialed := false
	d := &Dialer{
		Config: &Config{SetupTimeout: 50 * time.Millisecond},
		DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
			dialed = true
			return &FakeStreamConn{}, nil
		},
	}

	sess, err := d.Dial(context.Background(), "moqt://example.com:9000/live?token=abc", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrQueryNotSupported)
	assert.Nil(t, sess)
	assert.False(t, dialed, "the connection must not be opened")
}
