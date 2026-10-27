package quicgo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testALPN = "quicgo-test"

func TestResolveUDPAddr(t *testing.T) {
	tests := map[string]struct {
		addr     string
		wantHost string
		wantAddr string
		wantErr  bool
	}{
		"IPv4 and port":    {addr: "192.0.2.1:4433", wantHost: "192.0.2.1", wantAddr: "192.0.2.1:4433"},
		"bracketed IPv6":   {addr: "[2001:db8::1]:4433", wantHost: "2001:db8::1", wantAddr: "[2001:db8::1]:4433"},
		"named port":       {addr: "192.0.2.1:https", wantHost: "192.0.2.1", wantAddr: "192.0.2.1:443"},
		"host, IPv4 first": {addr: "localhost:4433", wantHost: "localhost", wantAddr: "127.0.0.1:4433"},
		"no port":          {addr: "example.com", wantErr: true},
		"empty":            {addr: "", wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			host, raddr, err := resolveUDPAddr(t.Context(), tt.addr)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantAddr, raddr.String())
		})
	}
}

// Resolving gives up with the context, however slow the resolver.
func TestResolveUDPAddr_ContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolveUDPAddr(ctx, "unresolvable.invalid:4433")

	assert.Error(t, err)
}

func TestDialEarly_ServerName(t *testing.T) {
	srv := startEchoServer(t)
	port := strconv.Itoa(srv.port)
	tests := map[string]struct {
		addr       string
		serverName string
	}{
		// The certificate names localhost alone, so the handshake verifies
		// only if the name is taken from the host dialed, not its IP address.
		"inferred from the host dialed": {addr: "localhost:" + port},
		"kept when set":                 {addr: "127.0.0.1:" + port, serverName: "localhost"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tlsConf := srv.clientTLS(tt.serverName)

			conn := dialTest(t, tt.addr, tlsConf)

			assert.Equal(t, "localhost", conn.ConnectionState().TLS.ServerName)
			assert.Equal(t, tt.serverName, tlsConf.ServerName, "the caller's tls.Config must not change")
		})
	}
}

func TestDialEarly_ClosesSocketWithConnection(t *testing.T) {
	srv := startEchoServer(t)
	conn := dialTest(t, srv.addr(), srv.clientTLS("localhost"))
	port := conn.LocalAddr().(*net.UDPAddr).Port

	require.NoError(t, conn.CloseWithError(0, "done"))

	// The port is free again once DialEarly closed its socket.
	assert.Eventually(t, func() bool {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})
		if err != nil {
			return false
		}
		_ = c.Close() // not actionable: the probe socket is only checked for binding
		return true
	}, 2*time.Second, 20*time.Millisecond)
}

func TestDialEarly_UnresolvableAddress(t *testing.T) {
	conn, err := DialEarly(t.Context(), "no-port", &tls.Config{NextProtos: []string{testALPN}}, nil)

	assert.Error(t, err)
	assert.Nil(t, conn)
}

// A server follows a client through every change of its address, as a phone
// moving between Wi-Fi and cellular makes. quic-go's server runs out of
// connection IDs after two such changes for a client with non-zero-length
// ones (quic-go#5965); DialEarly's zero-length ones avoid that.
func TestDialEarly_FollowsRepeatedAddressChanges(t *testing.T) {
	srv := startEchoServer(t)
	rb := startRebinder(t, srv.addr())
	conn := dialTest(t, rb.frontAddr(), srv.clientTLS("localhost"))
	require.NoError(t, roundTrip(t, conn, "before any change"))

	for change := 1; change <= 5; change++ {
		rb.rebind(t)

		err := roundTrip(t, conn, "after change "+strconv.Itoa(change))

		require.NoError(t, err, "the server stopped following the client at change %d", change)
	}
}

func dialTest(tb testing.TB, addr string, tlsConf *tls.Config) *quic.Conn {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), 5*time.Second)
	defer cancel()
	conn, err := DialEarly(ctx, addr, tlsConf, &quic.Config{MaxIdleTimeout: 10 * time.Second})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conn.CloseWithError(0, "test done") }) // not actionable: cleanup
	return conn
}

// roundTrip sends msg on a new stream and checks the server echoes it.
func roundTrip(tb testing.TB, conn *quic.Conn, msg string) error {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), 3*time.Second)
	defer cancel()
	str, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	if err := str.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	if _, err := str.Write([]byte(msg)); err != nil {
		return err
	}
	if err := str.Close(); err != nil {
		return err
	}
	got, err := io.ReadAll(str)
	if err != nil {
		return err
	}
	assert.Equal(tb, msg, string(got))
	return nil
}

// echoServer is a QUIC server on every local address that echoes each stream.
type echoServer struct {
	port int
	cert *x509.Certificate
}

func (s *echoServer) addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)) }

// clientTLS trusts the server's certificate, for serverName when it is set.
func (s *echoServer) clientTLS(serverName string) *tls.Config {
	roots := x509.NewCertPool()
	roots.AddCert(s.cert)
	return &tls.Config{RootCAs: roots, ServerName: serverName, NextProtos: []string{testALPN}, MinVersion: tls.VersionTLS13}
}

func startEchoServer(tb testing.TB) *echoServer {
	tb.Helper()
	cert, leaf := localhostCert(tb)
	ln, err := quic.ListenAddrEarly(":0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{testALPN},
		MinVersion:   tls.VersionTLS13,
	}, &quic.Config{MaxIdleTimeout: 10 * time.Second})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = ln.Close() }) // not actionable: cleanup
	// The server's goroutines end with the test: its context is cancelled
	// before cleanups run, and the listener and connections close with it.
	ctx := tb.Context()
	go func() {
		for {
			conn, err := ln.Accept(ctx)
			if err != nil {
				return // the test is over or the listener is closed
			}
			go func() {
				for {
					str, err := conn.AcceptStream(ctx)
					if err != nil {
						return // the test is over or the connection is closed
					}
					go func() {
						_, _ = io.Copy(str, str) // not actionable: the client checks the echo
						_ = str.Close()          // not actionable: as above
					}()
				}
			}()
		}
	}()
	return &echoServer{port: ln.Addr().(*net.UDPAddr).Port, cert: leaf}
}

// localhostCert is a self-signed certificate for localhost alone.
func localhostCert(tb testing.TB) (tls.Certificate, *x509.Certificate) {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(tb, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(tb, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

// rebinder relays a client's datagrams to a target from a socket it can
// swap, so the target sees the client's address change, as a NAT rebinding
// or a move to another network makes it.
type rebinder struct {
	front  *net.UDPConn
	target *net.UDPAddr

	mu     sync.Mutex
	up     *net.UDPConn
	client *net.UDPAddr
}

func startRebinder(tb testing.TB, target string) *rebinder {
	tb.Helper()
	ta, err := net.ResolveUDPAddr("udp", target)
	require.NoError(tb, err)
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	rb := &rebinder{front: front, target: ta}
	rb.rebind(tb)
	go rb.relayFromClient()
	tb.Cleanup(func() {
		_ = front.Close() // not actionable: cleanup
		rb.mu.Lock()
		defer rb.mu.Unlock()
		_ = rb.up.Close() // not actionable: cleanup
	})
	return rb
}

func (rb *rebinder) frontAddr() string { return rb.front.LocalAddr().String() }

// rebind moves the relay to a new socket and closes the old one, so the
// target sees a new address and the old one no longer answers.
func (rb *rebinder) rebind(tb testing.TB) {
	tb.Helper()
	up, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	rb.mu.Lock()
	old := rb.up
	rb.up = up
	rb.mu.Unlock()
	if old != nil {
		_ = old.Close() // not actionable: the old address is meant to go away
	}
	go rb.relayToClient(up)
}

func (rb *rebinder) relayFromClient() {
	buf := make([]byte, 2048)
	for {
		n, client, err := rb.front.ReadFromUDP(buf)
		if err != nil {
			return // the relay is closed
		}
		rb.mu.Lock()
		rb.client = client
		up := rb.up
		rb.mu.Unlock()
		_, _ = up.WriteToUDP(buf[:n], rb.target) // not actionable: a lost datagram is QUIC's to recover
	}
}

func (rb *rebinder) relayToClient(up *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, _, err := up.ReadFromUDP(buf)
		if err != nil {
			return // this socket was swapped out or the relay is closed
		}
		rb.mu.Lock()
		client := rb.client
		rb.mu.Unlock()
		_, _ = rb.front.WriteToUDP(buf[:n], client) // not actionable: as above
	}
}
