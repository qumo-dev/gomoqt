package msf

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBroadcastPath moqt.BroadcastPath = "/msf/catalog"

func TestBroadcast_ServeCatalogTrack_SendsEachChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	catalog, _ := subscribeTestCatalog(t, ctx, broadcast)

	assert.Empty(t, readTestCatalog(t, ctx, catalog).Tracks, "first group is the catalog at subscribe time")

	require.NoError(t, broadcast.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))
	assert.Equal(t, []string{"video"}, testTrackNames(readTestCatalog(t, ctx, catalog)))

	require.NoError(t, broadcast.RegisterTrack(Track{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))
	assert.Equal(t, []string{"video", "audio"}, testTrackNames(readTestCatalog(t, ctx, catalog)))

	assert.True(t, broadcast.RemoveTrack("video"))
	assert.Equal(t, []string{"audio"}, testTrackNames(readTestCatalog(t, ctx, catalog)))
}

func TestBroadcast_ServeCatalogTrack_SkipsUnchangedCatalog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	catalog, _ := subscribeTestCatalog(t, ctx, broadcast)
	readTestCatalog(t, ctx, catalog)

	// Setting the same catalog sends nothing, so the next group is the
	// change that follows it.
	require.NoError(t, broadcast.SetCatalog(Catalog{Version: CurrentVersion}))
	require.NoError(t, broadcast.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))

	assert.Equal(t, []string{"video"}, testTrackNames(readTestCatalog(t, ctx, catalog)))
}

func TestBroadcast_Close_EndsCatalogSubscription(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	catalog, _ := subscribeTestCatalog(t, ctx, broadcast)
	readTestCatalog(t, ctx, catalog)

	broadcast.Close()
	broadcast.Close() // idempotent

	_, err = catalog.AcceptGroup(ctx)
	assert.Error(t, err, "no group follows Close")
	assert.NoError(t, ctx.Err(), "the subscription ended rather than timing out")
}

func TestBroadcast_Close_LaterSubscriptionGetsOneGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	require.NoError(t, broadcast.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))
	broadcast.Close()
	catalog, _ := subscribeTestCatalog(t, ctx, broadcast)

	assert.Equal(t, []string{"video"}, testTrackNames(readTestCatalog(t, ctx, catalog)))
	_, err = catalog.AcceptGroup(ctx)
	assert.Error(t, err)
	assert.NoError(t, ctx.Err(), "the subscription ended rather than timing out")
}

func TestBroadcast_ServeCatalogTrack_EndsWithAnnouncement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	catalog, endAnnouncement := subscribeTestCatalog(t, ctx, broadcast)
	readTestCatalog(t, ctx, catalog)

	// A publisher that stops publishing ends the announcement, not the
	// Broadcast; the mux closes the catalog writer and the handler returns.
	endAnnouncement()

	_, err = catalog.AcceptGroup(ctx)
	assert.Error(t, err, "no group follows the announcement's end")
	assert.NoError(t, ctx.Err(), "the subscription ended rather than timing out")
}

// subscribeTestCatalog serves broadcast from a loopback MoQ server and returns
// a client subscription to its catalog track, and a function that ends the
// broadcast's announcement.
func subscribeTestCatalog(tb testing.TB, ctx context.Context, broadcast *Broadcast) (*moqt.TrackReader, context.CancelFunc) {
	tb.Helper()

	mux := moqt.NewTrackMux(0)
	announced, endAnnouncement := context.WithCancel(ctx)
	tb.Cleanup(endAnnouncement)
	mux.Publish(announced, testBroadcastPath, broadcast)
	server := &moqt.Server{
		Addr: testFreeAddr(tb),
		TLSConfig: &tls.Config{
			NextProtos:   []string{moqt.NextProtoMOQ},
			Certificates: []tls.Certificate{testCert(tb)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    moqt.HandleFunc(func(sess *moqt.Session) { <-sess.Context().Done() }),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, moqt.ErrServerClosed) {
			tb.Logf("server: %v", err)
		}
	}()
	tb.Cleanup(func() { _ = server.Close() })

	dialer := &moqt.Dialer{TLSConfig: &tls.Config{InsecureSkipVerify: true}}
	var sess *moqt.Session
	require.Eventually(tb, func() bool {
		s, err := dialer.Dial(ctx, "moqt://"+server.Addr, nil)
		if err != nil {
			return false
		}
		sess = s
		return true
	}, 5*time.Second, 50*time.Millisecond)
	tb.Cleanup(func() { _ = sess.CloseWithError(moqt.NoError, "test done") })

	catalog, err := sess.Subscribe(ctx, testBroadcastPath, DefaultCatalogTrackName, nil)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = catalog.Close() })
	return catalog, endAnnouncement
}

// readTestCatalog reads the next catalog group.
func readTestCatalog(tb testing.TB, ctx context.Context, catalog *moqt.TrackReader) Catalog {
	tb.Helper()

	group, err := catalog.AcceptGroup(ctx)
	require.NoError(tb, err)
	var raw []byte
	for frame := range group.Frames(moqt.NewFrame(1024)) {
		raw = append(raw, frame.Body()...)
	}
	parsed, err := ParseCatalog(raw)
	require.NoError(tb, err)
	return parsed
}

func testTrackNames(catalog Catalog) []string {
	names := make([]string, 0, len(catalog.Tracks))
	for _, track := range catalog.Tracks {
		names = append(names, track.Name)
	}
	return names
}

func testFreeAddr(tb testing.TB) string {
	tb.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(tb, err)
	addr := pc.LocalAddr().String()
	require.NoError(tb, pc.Close())
	return addr
}

// testCert returns the self-signed certificate the moqt tests use.
func testCert(tb testing.TB) tls.Certificate {
	tb.Helper()
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`)
	keyPEM := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(tb, err)
	return cert
}
