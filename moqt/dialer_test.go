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

func TestDialer_DialWebTransport_DefaultPath(t *testing.T) {
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

	sess, err := dialer.DialQUIC(context.Background(), "example.com:9000", "/", nil)
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

// TestDialer_Dial_PopulatesSessionPath verifies Session.Path reports the path
// that was actually dialed, on both bindings. The DialWebTransport case also
// pins the host-already-carries-a-scheme branch, where the path argument is not
// part of the dialed target and Session.Path must follow the target, not it.
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

	t.Run("WebTransportHostCarryingSchemeIgnoresPathArgument", func(t *testing.T) {
		var target string
		d := newWebTransportDialer(&target)
		// The path argument is dropped when host already carries a scheme;
		// Session.Path must report the target that was dialed.
		sess, err := d.DialWebTransport(context.Background(), "https://example.com:443/from-host", "/from-arg", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

		assert.Equal(t, "https://example.com:443/from-host", target)
		assert.Equal(t, "/from-host", sess.Path())
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

// TestDialer_DialWebTransport_UnparsableTargetDoesNotReportIgnoredPath pins the
// fallback in dialedPath. When host already carries a scheme, DialWebTransport
// discards the path argument; if the target is then unparsable, Session.Path
// must not report that discarded argument — it would disagree with the
// connection, which is the disagreement the tracking exists to prevent.
// Reachable through DialWebTransportFunc, a documented extension point that may
// accept targets net/url rejects.
func TestDialer_DialWebTransport_UnparsableTargetDoesNotReportIgnoredPath(t *testing.T) {
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

	// A space in the authority makes url.Parse fail.
	sess, err := d.DialWebTransport(context.Background(), "https://exa mple.com/from-host", "/from-arg", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	assert.Equal(t, "https://exa mple.com/from-host", target)
	assert.NotEqual(t, "/from-arg", sess.Path(), "must not report the discarded path argument")
	assert.Equal(t, "/", sess.Path())
}

// TestDialer_DialQUIC_RootsPath verifies a non-empty unrooted path is rooted,
// as url.Parse would for the equivalent "moqt://" URL. Left unrooted it would
// break Session.Path's documented contract and be sent verbatim as the SETUP
// Path parameter, which the peer rejects.
func TestDialer_DialQUIC_RootsPath(t *testing.T) {
	newDialer := func() *Dialer {
		return &Dialer{
			Config: &Config{SetupTimeout: 50 * time.Millisecond},
			DialQUICFunc: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (StreamConn, error) {
				conn := &FakeStreamConn{}
				conn.AcceptStreams = []biStreamResult{{Err: context.Canceled}}
				conn.AcceptUniStreams = []recvStreamResult{{Err: context.Canceled}}
				return conn, nil
			},
		}
	}

	for _, tc := range []struct{ name, in, want string }{
		{name: "Unrooted", in: "live/alice", want: "/live/alice"},
		{name: "Rooted", in: "/live/alice", want: "/live/alice"},
		{name: "Empty", in: "", want: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := newDialer().DialQUIC(context.Background(), "example.com:9000", tc.in, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

			assert.Equal(t, tc.want, sess.Path())
			require.NotEmpty(t, sess.Path())
			assert.Equal(t, byte('/'), sess.Path()[0], "the SETUP Path parameter must be rooted")
		})
	}
}
