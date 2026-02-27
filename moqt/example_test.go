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
		SetupHandler: moqt.SetupHandlerFunc(func(w moqt.SetupResponseWriter, r *moqt.SetupRequest) {
			// Select a supported version from the client's request
			if err := w.SelectVersion(moqt.DefaultServerVersion); err != nil {
				w.Reject(moqt.UnsupportedVersionErrorCode)
				return
			}
		}),
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

// ExampleExtension demonstrates how to work with MOQ protocol parameters.
func ExampleExtension() {
	// Create new parameters
	params := moqt.NewExtension()

	// Set various parameter types
	params.SetUint(1, 42)
	params.SetString(2, "example")
	params.SetBool(3, true)

	// Retrieve parameters
	if value, err := params.GetUint(1); err == nil {
		fmt.Printf("Parameter 1: %d\n", value)
	}

	if value, err := params.GetString(2); err == nil {
		fmt.Printf("Parameter 2: %s\n", value)
	}

	if value, err := params.GetBool(3); err == nil {
		fmt.Printf("Parameter 3: %t\n", value)
	}

	// Clone parameters
	clonedParams := params.Clone()
	fmt.Printf("Cloned parameters: %s\n", clonedParams.String())
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
