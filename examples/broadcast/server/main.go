package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt"
)

const (
	frameInterval = 100 * time.Millisecond
	openTimeout   = 100 * time.Millisecond
)

func main() {
	server := moqt.Server{
		Addr: "localhost:4469",
		TLSConfig: &tls.Config{
			NextProtos:         []string{moqt.NextProtoH3, moqt.NextProtoMOQ},
			Certificates:       []tls.Certificate{generateCert()},
			InsecureSkipVerify: true, // TODO: Not recommended for production
		},
		QUICConfig: &quic.Config{
			Allow0RTT:       true,
			EnableDatagrams: true,
		},
		Logger: slog.Default(),
	}

	// Serve moq over webtransport
	handler := &moqt.WebTransportHandler{}
	http.Handle("/broadcast", handler)

	moqt.PublishFunc(context.Background(), "/server.broadcast", func(tw *moqt.TrackWriter) {
		frame := moqt.NewFrame(1024)
		ticker := time.NewTicker(frameInterval)
		defer ticker.Stop()

		for {
			select {
			case <-tw.Context().Done():
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
				gw, err := tw.OpenGroup(ctx)
				cancel()
				if err != nil {
					slog.Error("failed to open group", "error", err)
					if tw.Context().Err() != nil {
						return
					}
					continue
				}

				frame.Reset()
				frame.Write([]byte("FRAME " + gw.GroupSequence().String()))
				err = gw.WriteFrame(frame)
				if err != nil {
					gw.CancelWrite(moqt.InternalGroupErrorCode) // TODO: Handle error properly
					slog.Error("failed to write frame", "error", err)
					return
				}

				// TODO: Release the frame after writing
				// This is important to avoid memory leaks
				gw.Close()
			}
		}
	})

	err := server.ListenAndServe()
	if err != nil {
		slog.Error("failed to listen and serve", "error", err)
	}
}

func generateCert() tls.Certificate {
	// Find project root by looking for go.mod file
	projectRoot, err := findProjectRoot()
	if err != nil {
		panic(err)
	}

	// Load certificates from the examples/cert directory (project root)
	certPath := filepath.Join(projectRoot, "examples", "cert", "localhost.pem")
	keyPath := filepath.Join(projectRoot, "examples", "cert", "localhost-key.pem")

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
