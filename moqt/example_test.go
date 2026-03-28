package moqt_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"

	"github.com/okdaichi/gomoqt/moqt"
)

// Example demonstrates how to create and configure a basic MOQ server.
func Example() {
	// Create a minimal TLS configuration (in production, use proper certificates)
	tlsConfig := &tls.Config{
		// Configure your certificates here
		MinVersion: tls.VersionTLS13,
	}

	// Create the MOQ server
	server := &moqt.Server{
		Addr:      ":4433",
		TLSConfig: tlsConfig,
	}

	// Start serving (this blocks, so typically run in a goroutine)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient demonstrates how to create a MOQ client and establish a connection.
func ExampleClient() {
	// Create a TLS configuration
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Only for testing!
		MinVersion:         tls.VersionTLS13,
	}

	// Create the client
	client := &moqt.Client{
		TLSConfig: tlsConfig,
	}

	// Create a track multiplexer for routing
	mux := moqt.NewTrackMux()

	// Connect to the server (use "https://" for WebTransport or "moqt://" for QUIC)
	session, err := client.Dial(context.Background(), "https://localhost:4433", mux)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = session.CloseWithError(moqt.NoError, "done") }()

	fmt.Println("Connected to MOQ server")
}

// ExampleTrackMux demonstrates how to use the track multiplexer for publishing tracks.
func ExampleTrackMux() {
	// Create a new multiplexer
	mux := moqt.NewTrackMux()

	// Publish a track with a handler
	ctx := context.Background()
	mux.PublishFunc(ctx, "example/path", func(tw *moqt.TrackWriter) {
		// Handle track subscription and write data
		fmt.Println("Track writer ready for: example/path")
	})

	// The mux can now route subscription requests to the appropriate handlers
	fmt.Println("Mux configured with track handler")
}
