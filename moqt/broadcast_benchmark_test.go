package moqt

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// BenchmarkBroadcastServer_HighLoad benchmarks a realistic broadcast scenario
// with 100 concurrent clients subscribing to a 30fps stream with 1-10KB frames
func BenchmarkBroadcastServer_HighLoad(b *testing.B) {
	clients := []int{10, 50, 100}

	for _, numClients := range clients {
		b.Run(fmt.Sprintf("clients-%d", numClients), func(b *testing.B) {
			ctx := b.Context()

			// Setup server
			server, addr := setupBroadcastServer(b, ctx)
			defer server.Close()

			// Metrics
			var (
				framesReceived atomic.Int64
				bytesReceived  atomic.Int64
				errors         atomic.Int64
			)

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				var wg sync.WaitGroup
				clientCtx, clientCancel := context.WithTimeout(ctx, 10*time.Second)

				// Start concurrent clients
				for i := range numClients {
					wg.Add(1)
					go func(clientID int) {
						defer wg.Done()
						err := runBroadcastClient(clientCtx, addr, &framesReceived, &bytesReceived)
						if err != nil {
							errors.Add(1)
						}
					}(i)
				}

				// Wait for all clients to complete
				wg.Wait()
				clientCancel()
			}

			b.StopTimer()

			// Report metrics
			b.ReportMetric(float64(framesReceived.Load())/float64(b.N), "frames/op")
			b.ReportMetric(float64(bytesReceived.Load())/float64(b.N), "bytes/op")
			b.ReportMetric(float64(errors.Load())/float64(b.N), "errors/op")
		})
	}
}

// BenchmarkBroadcastServer_Profile runs a 60-second profiling scenario
// This is designed to be run with -cpuprofile and -memprofile flags
func BenchmarkBroadcastServer_Profile(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping long-running profile benchmark in short mode")
	}

	ctx := b.Context()

	// Setup server
	server, addr := setupBroadcastServer(b, ctx)
	defer server.Close()

	const (
		numClients = 100
		duration   = 60 * time.Second
	)

	var (
		framesReceived atomic.Int64
		bytesReceived  atomic.Int64
	)

	b.ResetTimer()

	profileCtx, profileCancel := context.WithTimeout(ctx, duration)
	defer profileCancel()

	var wg sync.WaitGroup

	// Start concurrent clients
	for i := range numClients {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			_ = runBroadcastClient(profileCtx, addr, &framesReceived, &bytesReceived)
		}(i)
	}

	// Wait for profile duration
	<-profileCtx.Done()
	wg.Wait()

	b.StopTimer()

	// Report comprehensive metrics
	totalFrames := framesReceived.Load()
	totalBytes := bytesReceived.Load()
	elapsed := duration.Seconds()

	b.ReportMetric(float64(totalFrames)/elapsed, "frames/sec")
	b.ReportMetric(float64(totalBytes)/elapsed/1024/1024, "MB/sec")
	b.ReportMetric(float64(totalFrames)/float64(numClients), "frames/client")

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.ReportMetric(float64(m.Alloc)/1024/1024, "MB-alloc")
	b.ReportMetric(float64(m.Sys)/1024/1024, "MB-sys")
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
}

// BenchmarkBroadcastServer_FrameSizes tests different frame sizes
func BenchmarkBroadcastServer_FrameSizes(b *testing.B) {
	frameSizes := []int{100, 1024, 10240} // 100B, 1KB, 10KB

	for _, frameSize := range frameSizes {
		b.Run(fmt.Sprintf("size-%d", frameSize), func(b *testing.B) {
			ctx := b.Context()

			server, addr := setupBroadcastServerWithFrameSize(b, ctx, frameSize)
			defer server.Close()

			const numClients = 10
			var framesReceived, bytesReceived atomic.Int64

			b.SetBytes(int64(frameSize))
			b.ResetTimer()

			for range b.N {
				var wg sync.WaitGroup
				clientCtx, clientCancel := context.WithTimeout(ctx, 5*time.Second)

				for range numClients {
					wg.Go(func() {
						_ = runBroadcastClient(clientCtx, addr, &framesReceived, &bytesReceived)
					})
				}

				wg.Wait()
				clientCancel()
			}
		})
	}
}

func setupBroadcastServer(b *testing.B, ctx context.Context) (*http.Server, string) {
	return setupBroadcastServerWithFrameSize(b, ctx, 1024)
}

func setupBroadcastServerWithFrameSize(b *testing.B, ctx context.Context, frameSize int) (*http.Server, string) {
	b.Helper()

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		b.Fatalf("failed to find available port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	moqtServer := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{"h3", "moq-00"},
			Certificates:       []tls.Certificate{generateTestCert(b)},
			InsecureSkipVerify: true,
		},
		QUICConfig: &quic.Config{
			Allow0RTT:       true,
			EnableDatagrams: true,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Setup HTTP handler for WebTransport
	mux := http.NewServeMux()
	upgrader := Upgrader{}
	mux.HandleFunc("/broadcast", func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r)
	})

	httpServer := &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: moqtServer.TLSConfig,
	}

	// Register broadcast handler
	PublishFunc(ctx, "/server.broadcast", func(tw *TrackWriter) {
		frame := NewFrame(frameSize)
		ticker := time.NewTicker(33 * time.Millisecond) // ~30fps
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gw, err := tw.OpenGroup()
				if err != nil {
					return
				}

				frame.Reset()
				// Write realistic frame data
				data := make([]byte, frameSize)
				for i := range data {
					data[i] = byte(gw.GroupSequence() % 256)
				}
				frame.Write(data)

				err = gw.WriteFrame(frame)
				if err != nil {
					gw.CancelWrite(InternalGroupErrorCode)
					return
				}

				gw.Close()
			}
		}
	})

	// Start server in background
	go func() {
		err := moqtServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			b.Logf("server error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	return httpServer, "https://" + addr + "/broadcast"
}

func runBroadcastClient(ctx context.Context, serverAddr string, framesReceived, bytesReceived *atomic.Int64) error {
	client := Client{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	sess, err := client.Dial(ctx, serverAddr, nil)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer sess.CloseWithError(NoError, "client done")

	annRecv, err := sess.AcceptAnnounce("/")
	if err != nil {
		return fmt.Errorf("failed to accept announce: %w", err)
	}
	defer annRecv.Close()

	// Wait for first announcement
	ann, err := annRecv.ReceiveAnnouncement(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive announcement: %w", err)
	}

	if !ann.IsActive() {
		return fmt.Errorf("announcement not active")
	}

	tr, err := sess.Subscribe(ann.BroadcastPath(), "index", nil)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer tr.Close()

	// Read frames until context is done
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		gr, err := tr.AcceptGroup(ctx)
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				return nil
			}
			return fmt.Errorf("failed to accept group: %w", err)
		}

		frame := NewFrame(0)
		for {
			err := gr.ReadFrame(frame)
			if err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("failed to read frame: %w", err)
			}

			framesReceived.Add(1)
			bytesReceived.Add(int64(frame.Len()))
		}
	}
}

func generateTestCert(b *testing.B) tls.Certificate {
	b.Helper()

	// Use in-memory self-signed certificate for testing
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
	if err != nil {
		b.Fatalf("failed to load test certificate: %v", err)
	}
	return cert
}
