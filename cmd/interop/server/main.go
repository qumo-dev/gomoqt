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

	"github.com/okdaichi/gomoqt/moqt"
	"github.com/okdaichi/gomoqt/quic"
)

func main() {
	addr := flag.String("addr", ":9000", "server address")
	flag.Parse()

	if err := mkcert(); err != nil {
		fmt.Fprintf(os.Stderr, "Setting up certificatesfailed\n  Error: %v\n", err)
		return
	}

	// Print startup message directly
	fmt.Printf("[OK] Started on %s\n", *addr)

	server := moqt.Server{
		Addr: *addr,
		TLSConfig: &tls.Config{
			NextProtos:         []string{"h3", "moq-00"},
			Certificates:       []tls.Certificate{generateCert()},
			InsecureSkipVerify: true, // TODO: Not recommended for production
		},
		QUICConfig: &quic.Config{
			Allow0RTT:       true,
			EnableDatagrams: true,
		},
		CheckHTTPOrigin: func(r *http.Request) bool {
			return true // TODO: Implement proper origin check
		},
	}

	serverDone := make(chan struct{}, 1)
	go func() {
		<-serverDone
		_ = server.Close()
	}()
	defer func() {
		select {
		case serverDone <- struct{}{}:
		default:
		}
	}()

	// Serve MOQ over WebTransport
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := server.HandleWebTransport(w, r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to serve moq over webtransport: %v\n", err)
			return
		}
	})

	moqt.HandleFunc("/", func(w moqt.SetupResponseWriter, r *moqt.SetupRequest) {
		fmt.Print("Setting up session")

		// Create a custom mux for this session
		mux := moqt.NewTrackMux()

		// Accept the session with the custom mux
		sess, err := moqt.Accept(w, r, mux)
		if err != nil {
			fmt.Printf("failed\n  Error: %v\n", err)
			w.Reject(moqt.ProtocolViolationErrorCode)
			return
		}
		fmt.Println("ok")

		go func() {
			// Close the server when the session ends
			<-sess.Context().Done()

			select {
			case serverDone <- struct{}{}:
			default:
			}
		}()

		path := moqt.BroadcastPath("/interop/server")

		// Wait for publish handler to finish. Use a buffered channel to support
		// non-blocking notification so multiple handler invocations won't cause
		// panics on close.
		doneCh := make(chan struct{}, 1)
		mux.PublishFunc(context.Background(), path, func(tw *moqt.TrackWriter) {
			fmt.Printf("Serving a track: %s,%s\n", string(path), string(tw.TrackName))

			group, err := tw.OpenGroup()
			if err != nil {
				fmt.Printf("Opening group... failed\n  Error: %v\n", err)
				return
			}
			fmt.Println("Opening group... ok")

			defer group.Close()

			frame := moqt.NewFrame(1024)
			_, _ = frame.Write([]byte("HELLO"))

			err = group.WriteFrame(frame)
			if err != nil {
				fmt.Printf("Writing frame to client... failed\n  Error: %v\n", err)
				return
			}

			fmt.Println("Writing frame to client... ok")

			// Close the group to send FIN.
			group.Close()

			// Signal that handler has been invoked (non-blocking)
			select {
			case doneCh <- struct{}{}:
			default:
			}
		})

		// Wait for the broadcast to be published or timeout.
		select {
		case <-doneCh:
			// Published normally
		case <-time.After(5 * time.Second):
			fmt.Println("publish handler did not complete in time; continuing")
		}

		// Discover broadcasts from client
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
		// Subscribe to the announced broadcast. Subscribe blocks until the
		// subscription is established or an error occurs. No artificial sleeps.
		track, err := sess.Subscribe(ann.BroadcastPath(), "", nil)
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
		err = group.ReadFrame(frame)
		if err != nil {
			if err == io.EOF {
				return // Group closed by client
			}
			fmt.Printf("failed\n  Error: %v\n", err)
			return
		}
		fmt.Printf("ok (payload: %s)\n", string(frame.Body()))

		// Allow the session to finish cleanly by waiting for the session
		// context to finish or by deferring session close; we avoid
		// arbitrary sleeps.

		fmt.Print("Closing session...")
		_ = sess.CloseWithError(moqt.NoError, "no error")
		fmt.Println("ok")
	})

	fmt.Println("Listening...")
	err := server.ListenAndServe()
	// Ignore expected shutdown errors
	if err != nil && err != moqt.ErrServerClosed && err.Error() != "quic: server closed" {
		fmt.Fprintf(os.Stderr, "failed to listen and serve: %v\n", err)
		os.Exit(1)
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
