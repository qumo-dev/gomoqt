package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	// sync not needed here (we replaced WaitGroup with channel-based sync)
	"time"
	// "sync" is not used directly here; it may be needed in future changes

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt"
)

func main() {
	addr := flag.String("addr", ":9000", "server address")
	flag.Parse()

	if err := mkcert(); err != nil {
		fmt.Fprintf(os.Stderr, "Setting up certificatesfailed\n  Error: %v\n", err)
		return
	}

	serverDone := make(chan struct{}, 1)
	mux := moqt.NewTrackMux(0)

	// Print startup message directly
	fmt.Printf("[OK] Started on %s\n", *addr)

	fetchHandler := moqt.FetchHandlerFunc(func(w *moqt.GroupWriter, r *moqt.FetchRequest) {
		frame := moqt.NewFrame(1024)
		_, _ = frame.Write([]byte("HELLO"))
		_ = w.WriteFrame(frame)
	})

	server := moqt.Server{
		Addr: *addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{moqt.NextProtoH3, moqt.NextProtoMOQ},
			Certificates:       []tls.Certificate{generateCert()},
			InsecureSkipVerify: true, // TODO: Not recommended for production
		},
		QUICConfig: &quic.Config{
			Allow0RTT:       true,
			EnableDatagrams: true,
		},
		NextSessionURI: "https://next.example.com",
		FetchHandler:   fetchHandler,
		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
			runInteropSession(sess, mux, serverDone)
		}),
	}

	go func() {
		<-serverDone
		fmt.Println("Shutting down with GOAWAY...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
		}
		fmt.Println("Shutdown complete")
	}()
	defer func() {
		select {
		case serverDone <- struct{}{}:
		default:
		}
	}()

	handler := &moqt.WebTransportHandler{
		CheckOrigin:  func(r *http.Request) bool { return true },
		TrackMux:     mux,
		FetchHandler: fetchHandler,
		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
			runInteropSession(sess, mux, serverDone)
		}),
	}

	// Serve MOQ over WebTransport
	http.Handle("/", handler)

	fmt.Println("Listening...")
	err := server.ListenAndServe()
	// Ignore expected shutdown errors
	if err != nil && err != moqt.ErrServerClosed && err.Error() != "quic: server closed" {
		fmt.Fprintf(os.Stderr, "failed to listen and serve: %v\n", err)
		os.Exit(1)
	}
}

func runInteropSession(sess *moqt.Session, mux *moqt.TrackMux, serverDone chan struct{}) {

	path := moqt.BroadcastPath("/interop/server")
	doneCh := make(chan struct{}, 1)

	mux.PublishFunc(context.Background(), path, func(tw *moqt.TrackWriter) {
		fmt.Printf("Serving a track: %s,%s\n", string(path), string(tw.TrackName))

		group, err := tw.OpenGroup()
		if err != nil {
			fmt.Printf("Opening group... failed\n  Error: %v\n", err)
			return
		}
		defer group.Close()
		fmt.Println("Opening group... ok")

		frame := moqt.NewFrame(1024)
		_, _ = frame.Write([]byte("HELLO"))

		if err = group.WriteFrame(frame); err != nil {
			fmt.Printf("Writing frame to client... failed\n  Error: %v\n", err)
			return
		}
		fmt.Println("Writing frame to client... ok")

		select {
		case doneCh <- struct{}{}:
		default:
		}
	})

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		fmt.Println("publish handler did not complete in time; continuing")
	}

	fmt.Print("Accepting client announcements...")
	anns, err := sess.AcceptAnnounce("/")
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	defer anns.Close()
	fmt.Println("ok")

	fmt.Print("Receiving announcement...")
	ann, err := anns.ReceiveAnnouncement(context.Background())
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Println("ok")

	fmt.Printf("Discovered broadcast: %s\n", string(ann.BroadcastPath()))

	fmt.Print("Subscribing to broadcast...")
	track, err := sess.Subscribe(context.Background(), ann.BroadcastPath(), "", nil)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	defer track.Close()
	fmt.Println("ok")

	fmt.Print("Accepting group...")
	group, err := track.AcceptGroup(context.Background())
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Println("ok")

	fmt.Print("Reading the first frame from client...")
	frame := moqt.NewFrame(1024)
	if err = group.ReadFrame(frame); err != nil {
		if err == io.EOF {
			return
		}
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Printf("ok (payload: %s)\n", string(frame.Body()))

	// Probe client bitrate (server → client direction)
	fmt.Print("Probing client bitrate...")
	probeCh, err := sess.Probe(1_000_000)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	probeResult, ok := <-probeCh
	if !ok {
		fmt.Printf("failed\n  Error: probe stream closed without result\n")
		return
	}
	fmt.Printf("ok (measured: %d bps)\n", probeResult.Bitrate)

	// Signal the server to start graceful shutdown (sends GOAWAY to all sessions).
	select {
	case serverDone <- struct{}{}:
	default:
	}

	// Wait for the session to close (client disconnects after receiving GOAWAY).
	select {
	case <-sess.Context().Done():
		fmt.Println("Session closed after GOAWAY")
	case <-time.After(10 * time.Second):
		fmt.Println("Timed out waiting for session close after GOAWAY")
	}
}

func generateCert() tls.Certificate {
	// Find project root by looking for go.mod file
	root, err := findRootDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find project root: %v\n", err)
		os.Exit(1)
	}

	// Load certificates from the interop/cert directory (project root)
	certPath := filepath.Join(root, "cmd", "interop", "server", "localhost.pem")
	keyPath := filepath.Join(root, "cmd", "interop", "server", "localhost-key.pem")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load certificates: %v\n", err)
		os.Exit(1)
	}
	return cert
}

// findRootDir searches for the project root by looking for go.mod file
func findRootDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", os.ErrNotExist
}

func mkcert() error {
	// Resolve paths from project root so the program works regardless of CWD
	root, err := findRootDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find project root for mkcert: %v\n", err)
		return err
	}

	serverCertPath := filepath.Join(root, "cmd", "interop", "server", "localhost.pem")

	// Check if server certificates exist
	if _, err := os.Stat(serverCertPath); os.IsNotExist(err) {
		fmt.Print("Setting up certificates...")
		cmd := exec.Command("mkcert", "-cert-file", "localhost.pem", "-key-file", "localhost-key.pem", "localhost", "127.0.0.1", "::1")
		// Ensure mkcert runs in the server directory where cert files should be generated
		cmd.Dir = filepath.Join(root, "cmd", "interop", "server")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			fmt.Printf("failed\n  Error: %v\n", err)
			return err
		}
		fmt.Println("ok")
	}
	return nil
}
