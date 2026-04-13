package main

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/okdaichi/gomoqt/moqt"
	"github.com/quic-go/quic-go"
)

func main() {
	server := moqt.Server{
		Addr: "localhost:4444",
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
	upgrader := &moqt.WebTransportHandler{
		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
			mux := moqt.NewTrackMux()

			anns, err := sess.AcceptAnnounce("/")
			if err != nil {
				slog.Error("failed to open announce stream", "error", err)
			}

			for {
				ann, err := anns.ReceiveAnnouncement(context.Background())
				if err != nil {
					slog.Error("failed to receive announcements", "error", err)
					break
				}

				go func(ann *moqt.Announcement) {
					if !ann.IsActive() {
						return
					}

					tr, err := sess.Subscribe(context.Background(), ann.BroadcastPath(), "index", nil)
					if err != nil {
						slog.Error("failed to open track stream", "error", err)
						return
					}

					mux.Announce(ann, moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) {
						defer tr.Close()

						for {
							gr, err := tr.AcceptGroup(context.Background())
							if err != nil {
								slog.Error("failed to accept group", "error", err)
								return
							}

							go func(gr *moqt.GroupReader) {
								gw, err := tw.OpenGroup()
								if err != nil {
									slog.Error("failed to open group", "error", err)
									return
								}
								defer gw.Close()

								defer gr.CancelRead(moqt.InternalGroupErrorCode)
								frame := moqt.NewFrame(0)
								for {
									err := gr.ReadFrame(frame)
									if err != nil {
										if err == io.EOF {
											return
										}
										slog.Error("failed to accept frame", "error", err)
										return
									}

									err = gw.WriteFrame(frame)
									if err != nil {
										slog.Error("failed to write frame", "error", err)
										return
									}

									// TODO: Release the frame after writing
									// This is important to avoid memory leaks
								}
							}(gr)

						}
					}))
				}(ann)
			}
		}),
	}

	http.Handle("/echo", upgrader)

	err := server.ListenAndServeTLS("../../cert/localhost.pem", "../../cert/localhost-key.pem")
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
