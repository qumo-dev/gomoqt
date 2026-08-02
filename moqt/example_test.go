package moqt_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"github.com/qumo-dev/gomoqt/moqt"
)

// This file illustrates the main usage scenarios of the moqt package.
//
// Network-bound examples are shown without an // Output: line, so the Go tool
// compiles them but does not execute them; they require a live MOQ peer.
// ExampleFrame is a pure, runnable example.

// Example demonstrates how to create and configure a basic MOQ server.
//
// A Server accepts native QUIC sessions (scheme "moqt://"). Handler is invoked
// once per accepted session; TrackMux routes SUBSCRIBE requests to the
// registered track handlers, and FetchHandler serves one-off FETCH requests.
func Example() {
	// Create a minimal TLS configuration (in production, use proper certificates)
	tlsConfig := &tls.Config{
		// Configure your certificates here
		MinVersion: tls.VersionTLS13,
	}

	// Track multiplexer routes announcements and subscriptions to handlers.
	mux := moqt.NewTrackMux(0)
	ctx := context.Background()
	mux.PublishFunc(ctx, "/live/cam-1", func(tw *moqt.TrackWriter) {
		// Called once per subscriber; write media here. See ExampleGroupWriter.
		defer tw.Close()
	})

	// Create the MOQ server
	server := &moqt.Server{
		Addr:      ":4433",
		TLSConfig: tlsConfig,
		TrackMux:  mux,
		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
			defer func() { _ = sess.CloseWithError(moqt.NoError, "") }()
			// The server closes the session when the Handler returns, so block
			// until the session ends to keep serving subscription/fetch traffic
			// (the TrackMux and FetchHandler run on their own streams).
			<-sess.Context().Done()
		}),
		FetchHandler: moqt.FetchHandlerFunc(func(w *moqt.GroupWriter, r *moqt.FetchRequest) {
			defer w.Close()
			// Serve a single requested group. See ExampleSession_Fetch.
		}),
	}

	// Start serving (this blocks, so typically run in a goroutine)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient demonstrates how to create a MOQ client and establish a session.
//
// Dial selects the transport from the URL scheme: "moqt://" for native QUIC and
// "https://" for WebTransport.
func ExampleClient() {
	// Create a TLS configuration
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Only for testing!
		MinVersion:         tls.VersionTLS13,
	}

	// Create the client
	client := &moqt.Dialer{
		TLSConfig: tlsConfig,
	}

	// Create a track multiplexer for routing
	mux := moqt.NewTrackMux(0)

	// Connect to the server (use "https://" for WebTransport or "moqt://" for QUIC)
	session, err := client.Dial(context.Background(), "https://localhost:4433", mux)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = session.CloseWithError(moqt.NoError, "done") }()

	fmt.Println("Connected to MOQ server")
}

// ExampleTrackMux demonstrates how to use the track multiplexer for publishing tracks.
//
// The handler runs once for each subscriber; cancel the context to withdraw the
// track (existing subscribers are then closed).
func ExampleTrackMux() {
	// Create a new multiplexer
	mux := moqt.NewTrackMux(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // withdraw the announcement when done publishing

	// Publish a track with a handler. Paths are slash-prefixed.
	mux.PublishFunc(ctx, "/example/path", func(tw *moqt.TrackWriter) {
		defer tw.Close()
		// A subscriber arrived. See ExampleTrackWriter for writing media.
	})

	fmt.Println("Mux configured with track handler")
}

// ExampleTrackMux_publish shows a fan-out publisher writing live groups.
//
// Publish a handler that loops over groups, opening a GroupWriter per group and
// writing frames into it. Each connected subscriber receives its own copy of the
// stream through the same handler invocation.
func ExampleTrackMux_publish() {
	mux := moqt.NewTrackMux(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux.PublishFunc(ctx, "/live/stream", func(tw *moqt.TrackWriter) {
		defer tw.Close()

		frame := moqt.NewFrame(1500) // reused across frames
		for {
			if tw.Context().Err() != nil {
				return // subscriber or announcement gone
			}

			gw, err := tw.OpenGroup(ctx) // advances the group sequence automatically
			if err != nil {
				return
			}

			// Pack a few frames per group. See ExampleGroupWriter for details.
			for range 3 {
				frame.Reset()
				_, _ = frame.Write([]byte("payload"))
				if err := gw.WriteFrame(frame); err != nil {
					_ = gw.Close()
					return
				}
			}
			_ = gw.Close()

			time.Sleep(33 * time.Millisecond) // ~30 fps
		}
	})
}

// ExampleTrackWriter demonstrates writing groups and frames to a published track.
//
// A TrackWriter is supplied to a TrackHandler. Open a GroupWriter for each group,
// write frames into it, then close it to flush the group to the subscriber.
func ExampleTrackWriter() {
	ctx := context.Background()

	moqt.PublishFunc(ctx, "/demo/track", func(tw *moqt.TrackWriter) {
		defer tw.Close()

		// Optionally advertise publisher priority / ordering to the subscriber.
		_ = tw.WriteInfo(moqt.PublishInfo{Priority: moqt.TrackPriority(5)})

		frame := moqt.NewFrame(4096)
		for range 10 {
			gw, err := tw.OpenGroup(ctx)
			if err != nil {
				return
			}
			frame.Reset()
			_, _ = frame.Write([]byte("group payload"))
			_ = gw.WriteFrame(frame)
			_ = gw.Close() // close to flush the group
		}
	})
}

// ExampleGroupWriter shows how to fill and send a group of frames.
//
// Reuse a single Frame across writes to avoid allocations on the hot path.
func ExampleGroupWriter() {
	// Obtained from TrackWriter.OpenGroup in a real program.
	var gw *moqt.GroupWriter
	_ = gw // (illustrative; OpenGroup is shown in ExampleTrackWriter)

	frame := moqt.NewFrame(2048) // reused across frames
	for range 5 {
		frame.Reset()
		_, _ = frame.Write([]byte("frame payload"))
		// _ = gw.WriteFrame(frame)
	}
	// _ = gw.Close()
}

// ExampleGroupReader_Frames reads all frames of a group with a range iterator.
//
// Frames yields decoded frames until the group stream ends or the reader is
// canceled. Pass a reusable *Frame as the buffer to avoid per-frame allocations.
func ExampleGroupReader_Frames() {
	var gr *moqt.GroupReader
	_ = gr // (illustrative; obtained from TrackReader.AcceptGroup)

	buf := moqt.NewFrame(4096)
	for frame := range gr.Frames(buf) {
		_ = frame.Body() // consume frame payload
	}
}

// ExampleSession_Subscribe subscribes to a remote track and reads its groups.
//
// Subscribe blocks until the SUBSCRIBE_OK is received, then returns a
// TrackReader. Accept groups one at a time and read frames from each.
func ExampleSession_Subscribe() {
	var sess *moqt.Session // obtained from Dialer.Dial

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to "/live/cam-1", default config (latest groups, no start range).
	track, err := sess.Subscribe(ctx, "/live", moqt.TrackName("cam-1"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer track.Close()

	buf := moqt.NewFrame(4096)
	for {
		group, err := track.AcceptGroup(ctx)
		if err != nil {
			return
		}
		for frame := range group.Frames(buf) {
			_ = frame.Body() // consume media
		}
	}
}

// ExampleSession_AcceptAnnounce receives track announcements matching a prefix.
//
// AcceptAnnounce opens an announce stream for the given prefix; the returned
// AnnouncementReader yields each active or new track under that prefix.
func ExampleSession_AcceptAnnounce() {
	var sess *moqt.Session // obtained from Dialer.Dial

	reader, err := sess.AcceptAnnounce("/live/")
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	ctx := context.Background()
	for ann := range reader.Announcements(ctx) {
		fmt.Printf("announced: %s\n", ann.BroadcastPath())
	}
}

// ExampleAnnouncementReader_ReceiveAnnouncement receives a single announcement.
//
// Use ReceiveAnnouncement when you want to handle each announcement explicitly
// (e.g. to subscribe as soon as a matching track appears) rather than iterating.
func ExampleAnnouncementReader_ReceiveAnnouncement() {
	var reader *moqt.AnnouncementReader // obtained from Session.AcceptAnnounce

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ann, err := reader.ReceiveAnnouncement(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("new track: %s\n", ann.BroadcastPath())
}

// ExampleSession_Fetch fetches a single group by sequence.
//
// FETCH is a request/response for one specific group, in contrast to SUBSCRIBE
// which streams ongoing groups. Build a FetchRequest, optionally attach a
// context for cancellation/timeout, then read the returned group.
func ExampleSession_Fetch() {
	var sess *moqt.Session // obtained from Dialer.Dial

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := (&moqt.FetchRequest{
		BroadcastPath: "/vod/movie",
		TrackName:     moqt.TrackName("video"),
		GroupSequence: moqt.GroupSequence(42),
	}).WithContext(ctx)

	group, err := sess.Fetch(req)
	if err != nil {
		log.Fatal(err)
	}

	buf := moqt.NewFrame(4096)
	for frame := range group.Frames(buf) {
		_ = frame.Body()
	}
}

// ExampleSession_Probe measures the available outbound bitrate.
//
// Probe sends a target bitrate hint to the publisher and returns a channel of
// ProbeResult values carrying the publisher's measured bitrate. Calling Probe
// again on the same session updates the target.
func ExampleSession_Probe() {
	var sess *moqt.Session // obtained from Dialer.Dial

	results, err := sess.Probe(2_000_000) // hint 2 Mbps
	if err != nil {
		log.Fatal(err)
	}

	for r := range results {
		if r.Bitrate > 0 {
			fmt.Printf("measured bitrate: %d bps\n", r.Bitrate)
		}
	}
}

// ExampleBroadcast registers track handlers directly, without a TrackMux.
//
// A Broadcast is a flat name->handler registry. It is useful when routing is
// handled outside of the prefix tree that TrackMux maintains, or for tests.
func ExampleBroadcast() {
	bc := moqt.NewBroadcast()
	defer bc.Close()

	handler := moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) {
		defer tw.Close()
		// serve subscriber
	})

	if err := bc.Register(moqt.TrackName("audio"), handler); err != nil {
		log.Fatal(err)
	}
	if err := bc.Register(moqt.TrackName("video"), handler); err != nil {
		log.Fatal(err)
	}
}

// ExampleFrame is a runnable example showing how to build and reuse a Frame.
//
// A Frame wraps a reusable payload buffer; Write appends bytes and Reset clears
// the payload while preserving capacity for the next write.
func ExampleFrame() {
	frame := moqt.NewFrame(64)

	_, _ = frame.Write([]byte("hello"))
	_, _ = frame.Write([]byte(" world"))
	fmt.Printf("%s\n", frame.Body())
	fmt.Println(frame.Len())

	frame.Reset() // reuse the underlying buffer for the next frame
	fmt.Println(frame.Len())

	// Output:
	// hello world
	// 11
	// 0
}
