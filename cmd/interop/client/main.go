package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"time"

	"github.com/qumo-dev/gomoqt/moqt"
)

func main() {
	addr := flag.String("addr", "https://localhost:9000", "server URL for MOQ (https://host:port for WebTransport, moqt://host:port for native QUIC)")
	flag.Parse()

	goawayCh := make(chan string, 1)

	client := &moqt.Dialer{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, // interop uses local self-signed certs
		Config: &moqt.Config{
			SetupTimeout: 10 * time.Second,
		},
		OnGoaway: func(newSessionURI string) {
			fmt.Printf("Received GOAWAY (newSessionURI: %s)\n", newSessionURI)
			select {
			case goawayCh <- newSessionURI:
			default:
			}
		},
	}

	// Create a custom mux for this session (like http.NewServeMux())
	mux := moqt.NewTrackMux(0)

	fmt.Print("Connecting to server...")
	sess, err := client.Dial(context.Background(), *addr, mux)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Println("ok")

	defer func() {
		fmt.Print("Closing session...")
		_ = sess.CloseWithError(moqt.NoError, "no error")
		fmt.Println("ok")
	}()

	// Step 1: Accept announcements from server
	fmt.Print("Accepting server announcements...")
	anns, err := sess.AcceptAnnounce("/")
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	defer anns.Close()

	fmt.Println("ok")

	fmt.Print("Receiving announcement...")
	announceCtx, cancelReceive := context.WithTimeout(sess.Context(), 5*time.Second)
	defer cancelReceive()
	ann, err := anns.ReceiveAnnouncement(announceCtx)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Println("ok")

	fmt.Printf("Discovered broadcast: %s\n", string(ann.BroadcastPath()))

	// Step 2: Subscribe to the server's broadcast and receive data (in goroutine)
	fmt.Print("Subscribing to server broadcast...")
	track, err := sess.Subscribe(context.Background(), ann.BroadcastPath(), "", nil)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	defer track.Close()

	fmt.Println("ok")

	fmt.Print("Accepting group from server...")
	groupCtx, cancelGroup := context.WithTimeout(sess.Context(), 5*time.Second)
	defer cancelGroup()
	group, err := track.AcceptGroup(groupCtx)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Println("ok")

	fmt.Print("Reading the first frame from server...")
	frame := moqt.NewFrame(1024)

	err = group.ReadFrame(frame)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Printf("ok (payload: %s)\n", string(frame.Body()))

	// Step 3: Fetch a single group from the server
	fmt.Print("Fetching group from server...")
	gr, err := sess.Fetch(&moqt.FetchRequest{
		BroadcastPath: ann.BroadcastPath(),
		TrackName:     "",
		Priority:      0,
		GroupSequence: 0,
	})
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fetchFrame := moqt.NewFrame(1024)
	if err = gr.ReadFrame(fetchFrame); err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Printf("ok (payload: %s)\n", string(fetchFrame.Body()))

	// Step 4: Probe the server bitrate
	fmt.Print("Probing server bitrate...")
	probeResult, err := sess.Probe(1_000_000)
	if err != nil {
		fmt.Printf("failed\n  Error: %v\n", err)
		return
	}
	fmt.Printf("ok (measured: %d bps, rtt: %d ms)\n", probeResult.Bitrate, probeResult.RTT)

	// Channel to signal that the publish handler has completed
	doneCh := make(chan struct{}, 1)

	// Publish to the interop broadcast so server can discover it
	// Register the handler BEFORE dialing so it's ready when server requests announcements
	mux.PublishFunc(context.Background(), "/interop/client", func(tw *moqt.TrackWriter) {
		defer func() {
			select {
			case doneCh <- struct{}{}:
			default:
			}
		}()

		fmt.Print("Opening group...")
		group, err := tw.OpenGroup()
		if err != nil {
			fmt.Printf("failed\n  Error: %v\n", err)
			return
		}
		defer group.Close()
		fmt.Println("ok")

		fmt.Print("Writing frame to server...")
		frame := moqt.NewFrame(1024)
		_, _ = frame.Write([]byte("HELLO"))

		err = group.WriteFrame(frame)
		if err != nil {
			fmt.Printf("failed\n  Error: %v\n", err)
			return
		}
		fmt.Println("ok")
	})

	// Wait for the handler to cleanup
	select {
	case <-doneCh:
		// Handler completed normally
	case <-time.After(5 * time.Second):
		fmt.Println("publish handler did not complete in time")
	}

	// Wait for GOAWAY from server
	fmt.Print("Waiting for GOAWAY...")
	select {
	case uri := <-goawayCh:
		fmt.Printf("ok (newSessionURI: %s)\n", uri)
	case <-time.After(10 * time.Second):
		fmt.Println("failed (timed out)")
	}
}
