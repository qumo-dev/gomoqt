package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/okdaichi/gomoqt/moqt"
	"github.com/okdaichi/gomoqt/quic"
)

const CatalogTrackName = "catalog.json"

func main() {
	server := moqt.Server{
		Addr: "moqt.example.com:9000",
		TLSConfig: &tls.Config{
			NextProtos:         []string{moqt.NextProtoH3, moqt.NextProtoMOQ},
			Certificates:       []tls.Certificate{generateCert()},
			InsecureSkipVerify: true, // TODO: Not recommended for production
		},
		QUICConfig: &quic.Config{
			Allow0RTT:       true,
			EnableDatagrams: true,
		},
		CheckHTTPOrigin: func(r *http.Request) bool {
			slog.Info("HTTP Origin", "origin", r.Header.Get("Origin"))
			return true // TODO: Implement proper origin check
		},
		Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})),
	}

	path := "/hang"

	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		err := server.HandleWebTransport(w, r)
		if err != nil {
			slog.Error("Failed to serve moq over webtransport", "error", err)
		}
	})

	moqt.HandleFunc(path, func(w moqt.SetupResponseWriter, r *moqt.SetupRequest) {
		sess, err := moqt.Accept(w, r, nil)
		if err != nil {
			slog.Error("Failed to accept session", "error", err)
			return
		}

		slog.Info("New session established")

		ar, err := sess.AcceptAnnounce("/")
		if err != nil {
			return
		}

		for {
			ann, err := ar.ReceiveAnnouncement(context.Background())
			if err != nil {
				return
			}

			path := ann.BroadcastPath()

			if path.Extension() != ".hang" {
				// Ignore non-hang announcements
				ar.Close()
			}

			handler := newRelayHandler(path, sess)

			moqt.Announce(ann, handler)

			slog.Info("Announced new hang track",
				"path", path,
			)
		}
	})

	err := server.ListenAndServe()
	if err != nil {
		slog.Error("Failed to start server",
			"error", err,
		)
	}
}

func generateCert() tls.Certificate {
	// Find project root by looking for go.mod file
	projectRoot, err := findProjectRoot()
	if err != nil {
		panic(err)
	}

	// Load certificates from the examples/cert directory (project root)
	certPath := filepath.Join(projectRoot, "examples", "cert", "moqt.example.com.pem")
	keyPath := filepath.Join(projectRoot, "examples", "cert", "moqt.example.com-key.pem")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		panic(err)
	}
	return cert
}

// findProjectRoot searches for the project root by looking for go.mod file
func findProjectRoot() (string, error) {
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
