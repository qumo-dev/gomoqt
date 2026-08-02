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
// The data model has two nested units:
//
//   - A Frame is a single payload blob (one object/message).
//   - A Group is an ordered sequence of Frames. A group is the unit of
//     ordering and dropping on the wire — publishers emit whole groups and
//     subscribers consume whole groups.
//
// Tracks are addressed as (BroadcastPath, TrackName): the path is a slash
// namespace (e.g. "/live") and the name is the track within it (e.g. "cam-1").
//
// Network-bound examples below carry no // Output: line, so the Go tool
// compiles them but does not execute them — they require a live MOQ peer.
// ExampleFrame is pure and runnable.

// Example demonstrates a complete native-QUIC MOQ server.
//
// A Server listens for "moqt://" connections and wires together three
// independently-configurable concerns:
//
//   - Handler is the application callback invoked once per accepted session.
//   - TrackMux routes inbound SUBSCRIBE requests to registered publishers.
//   - FetchHandler serves one-shot FETCH requests for a single group.
//
// Important: the server closes a session when its Handler returns (see
// handleNativeQUIC). A Handler that wants the session's background stream
// handling (TrackMux subscriptions, FetchHandler fetches) to keep running must
// therefore block — here we wait on the session context, which is canceled when
// the peer closes the connection or the server shuts down.
//
// TLS 1.3 is required. ListenAndServe blocks; run it in a goroutine if you need
// to do other work in the same process. For WebTransport ("https://") servers,
// use WebTransportHandler with net/http instead of Server.
func Example() {
	tlsConfig := &tls.Config{
		// Configure your server certificate here.
		MinVersion: tls.VersionTLS13,
	}

	// TrackMux holds the announcement tree and routes subscribers to publishers.
	// hopID 0 marks this node as an endpoint (origin), not a relay.
	mux := moqt.NewTrackMux(0)
	ctx := context.Background()
	mux.PublishFunc(ctx, "/live/cam-1", func(tw *moqt.TrackWriter) {
		// Invoked once per subscriber. Write media here; see ExampleTrackWriter.
		defer tw.Close()
		<-tw.Context().Done() // keep the track open for the subscriber
	})

	server := &moqt.Server{
		Addr:      ":4433",
		TLSConfig: tlsConfig,
		TrackMux:  mux,
		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
			defer func() { _ = sess.CloseWithError(moqt.NoError, "") }()
			// Block so the session stays alive for subscription/fetch traffic.
			<-sess.Context().Done()
		}),
		FetchHandler: moqt.FetchHandlerFunc(func(w *moqt.GroupWriter, r *moqt.FetchRequest) {
			defer w.Close()
			// Serve the single requested group; see ExampleSession_Fetch.
		}),
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ExampleClient dials a MOQ session from a URL.
//
// Dialer.Dial selects the transport from the URL scheme: "moqt://" uses native
// QUIC and "https://" uses WebTransport. (DialWebTransport and DialQUIC select
// a transport explicitly when you already know which one you want.)
//
// The mux argument routes INBOUND tracks — subscriptions and announcements
// directed at this client when it also acts as a publisher. A pure subscriber
// that never publishes may pass nil.
//
// Always close a session when done. CloseWithError carries a code and a
// human-readable reason to the peer; NoError signals a clean shutdown.
func ExampleClient() {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // TESTING ONLY — use a real CA in production.
		MinVersion:         tls.VersionTLS13,
	}

	client := &moqt.Dialer{TLSConfig: tlsConfig}
	mux := moqt.NewTrackMux(0) // pass nil if you never publish from this client

	// "https://" → WebTransport; "moqt://" → native QUIC.
	session, err := client.Dial(context.Background(), "https://localhost:4433", mux)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = session.CloseWithError(moqt.NoError, "done") }()

	fmt.Println("Connected to MOQ server")
}

// ExampleTrackMux registers a publishable track on a TrackMux.
//
// A TrackMux keeps two indexes: a flat path→handler map for fast SUBSCRIBE
// lookup, and a prefix tree of announcements that notifies anyone who has
// opened an announce stream matching a prefix (see ExampleSession_AcceptAnnounce).
//
// PublishFunc is sugar: it creates an Announcement for the path and registers
// the function as a TrackHandler. The handler runs once PER subscriber; each
// subscriber gets its own TrackWriter. Cancel the context to withdraw the
// track — the announcement ends and existing subscribers are closed.
//
// Paths must begin with "/".
func ExampleTrackMux() {
	mux := moqt.NewTrackMux(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // withdraw the announcement when you stop publishing

	mux.PublishFunc(ctx, "/example/path", func(tw *moqt.TrackWriter) {
		defer tw.Close()
		// A subscriber arrived; see ExampleTrackWriter for writing media.
	})

	fmt.Println("Mux configured with track handler")
}

// ExampleTrackMux_publish is a live (streaming) publisher.
//
// The handler below runs for each subscriber and loops indefinitely, opening a
// new GroupWriter per group and packing frames into it. OpenGroup atomically
// advances the group sequence, so consecutive calls produce monotonically
// increasing group numbers without the caller tracking them.
//
// Because the same handler services every subscriber, the mux fans one logical
// track out to many viewers. Each subscriber's TrackWriter is independent, so a
// slow viewer does not block others — bounded only by the QUIC stream's
// per-subscriber flow control.
//
// Reuse a single Frame across writes: its buffer is retained across Reset
// calls, avoiding per-frame allocation on the publishing hot path.
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

			// Several frames per group; see ExampleGroupWriter.
			for range 3 {
				frame.Reset()
				_, _ = frame.Write([]byte("payload"))
				if err := gw.WriteFrame(frame); err != nil {
					_ = gw.Close()
					return
				}
			}
			_ = gw.Close() // finalize the group

			time.Sleep(33 * time.Millisecond) // ~30 fps
		}
	})
}

// ExampleTrackWriter shows the per-subscriber writing loop inside a handler.
//
// A TrackWriter is handed to a TrackHandler for each subscriber. Within it you:
//
//   - optionally call WriteInfo once to publish priority/ordering/latency hints
//     (these influence how the network schedules this track relative to others);
//   - open one GroupWriter per group with OpenGroup (or OpenGroupAt to pin a
//     specific sequence);
//   - WriteFrame into the group for each payload;
//   - Close the GroupWriter to finalize the group once you are done with it.
//
// tw.Context() is canceled when the subscriber leaves or the announcement ends,
// so loops should poll it (or pass it to OpenGroup) to stop promptly.
func ExampleTrackWriter() {
	ctx := context.Background()

	moqt.PublishFunc(ctx, "/demo/track", func(tw *moqt.TrackWriter) {
		defer tw.Close()

		// Advertise publisher-side parameters to the subscriber.
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
			_ = gw.Close() // finalize this group
		}
	})
}

// ExampleGroupWriter packs several frames into one group.
//
// A group is the atomic unit subscribers receive: frames within a group are
// delivered in order, and a publisher can drop a whole group at once via
// DropGroups to shed load. WriteFrame takes a *Frame and encodes it
// synchronously, so allocate one Frame before the loop and Reset it between
// writes — the buffer is safe to reuse once WriteFrame returns.
//
// Close the GroupWriter when the group is complete. Each group is its own
// stream, so closing one is not required before opening the next (groups may
// even overlap); closing finalizes the group so the subscriber can finish it.
func ExampleGroupWriter() {
	// In a real program gw comes from TrackWriter.OpenGroup (see ExampleTrackWriter).
	var gw *moqt.GroupWriter
	_ = gw

	frame := moqt.NewFrame(2048) // reused across frames
	for range 5 {
		frame.Reset()
		_, _ = frame.Write([]byte("frame payload"))
		// _ = gw.WriteFrame(frame)
	}
	// _ = gw.Close()
}

// ExampleGroupReader_Frames reads every frame of one group with a range loop.
//
// Frames returns an iterator that decodes frames until the group stream ends
// (clean EOF), the reader is canceled, or a stream error occurs. Pass a shared
// *Frame as the scratch buffer: each yielded frame reuses that buffer, so
// consume or copy the body before the next iteration.
//
// For lower-level control (e.g. custom error handling per frame) call ReadFrame
// directly in a loop instead.
func ExampleGroupReader_Frames() {
	// gr comes from TrackReader.AcceptGroup (see ExampleSession_Subscribe).
	var gr *moqt.GroupReader
	_ = gr

	buf := moqt.NewFrame(4096)
	for frame := range gr.Frames(buf) {
		_ = frame.Body() // consume before the next iteration overwrites buf
	}
}

// ExampleSession_Subscribe subscribes to a remote track and consumes its groups.
//
// Subscribe performs the SUBSCRIBE handshake: it opens a stream, sends the
// request, and blocks until SUBSCRIBE_OK (or an error). On success it returns a
// TrackReader. The (path, name) pair identifies the track; a nil config requests
// the live tail (latest groups) with default priority and no range filter.
//
// To customize delivery, pass a *SubscribeConfig: Priority affects relative
// scheduling, Ordered requests in-order group delivery, MaxLatency caps
// buffering, and StartGroup/EndGroup select a range (useful for seek/rewind).
//
// Then loop: AcceptGroup blocks for the next group, and group.Frames iterates
// its frames. The loop ends with an error when the track ends or the
// subscription is canceled.
func ExampleSession_Subscribe() {
	var sess *moqt.Session // from Dialer.Dial

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to the "cam-1" track under the "/live" namespace, live tail.
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

// ExampleSession_AcceptAnnounce discovers tracks advertised by the peer.
//
// AcceptAnnounce opens an announce stream for a prefix (which must start with
// "/" and, unless it is just "/", end with "/"). The returned AnnouncementReader
// yields an Announcement for every track the peer publishes under that prefix —
// both the tracks already active when the call is made and any added later. When
// a track is withdrawn, its Announcement's Done channel fires (see IsActive /
// AfterFunc on Announcement).
//
// This is the discovery mechanism: call it once per prefix you care about, then
// Subscribe to each announced path as it arrives.
func ExampleSession_AcceptAnnounce() {
	var sess *moqt.Session // from Dialer.Dial

	reader, err := sess.AcceptAnnounce("/live/")
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	for ann := range reader.Announcements(context.Background()) {
		fmt.Printf("announced: %s\n", ann.BroadcastPath())
	}
}

// ExampleAnnouncementReader_ReceiveAnnouncement receives one announcement.
//
// ReceiveAnnouncement blocks until an announcement is available or the supplied
// context / the reader's context is canceled. Use it (rather than the
// Announcements iterator) when you want explicit control over each step — for
// example, to subscribe to a track the instant it appears, or to enforce a
// timeout on the first announcement.
func ExampleAnnouncementReader_ReceiveAnnouncement() {
	var reader *moqt.AnnouncementReader // from Session.AcceptAnnounce

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
// FETCH is request/response for ONE specific group, unlike SUBSCRIBE which
// streams ongoing groups. It is the primitive for seek, rewind, and thumbnail
// style access: ask for group N of a track, read its frames, done.
//
// Build a FetchRequest with the (path, name) and the GroupSequence you want;
// WithContext attaches a deadline/cancellation source (the request otherwise
// uses the background context). The returned GroupReader behaves like a
// subscription's group — iterate Frames to read the payload.
func ExampleSession_Fetch() {
	var sess *moqt.Session // from Dialer.Dial

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
// Probe sends a target-bitrate hint to the publisher and returns a channel of
// ProbeResult values carrying the publisher's measured bitrate. The publisher
// drives the cadence; calling Probe again on the same session updates the
// target, reusing the existing probe stream when it is still alive (and opening
// a fresh one if not). The channel closes when the probe stream ends or the
// session terminates.
//
// Use the measured value to pick an encoding bitrate that fits the path, then
// re-probe when conditions change.
func ExampleSession_Probe() {
	var sess *moqt.Session // from Dialer.Dial

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

// ExampleBroadcast registers track handlers without a TrackMux.
//
// A Broadcast is a flat TrackName→handler registry with no prefix routing and
// no announcement tree. It is the lower-level building block: useful when you
// implement your own routing/discovery, in tests, or when you serve a fixed set
// of tracks. For prefix-based announcement and SUBSCRIBE routing, prefer
// TrackMux (see ExampleTrackMux).
func ExampleBroadcast() {
	bc := moqt.NewBroadcast()
	defer bc.Close()

	serve := moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) {
		defer tw.Close()
		// serve this subscriber
	})

	if err := bc.Register(moqt.TrackName("audio"), serve); err != nil {
		log.Fatal(err)
	}
	if err := bc.Register(moqt.TrackName("video"), serve); err != nil {
		log.Fatal(err)
	}
}

// ExampleFrame is a runnable example of building and reusing a Frame.
//
// A Frame wraps a reusable payload buffer. Write appends bytes (growing the
// buffer as needed); Body returns the current payload; Len is its length; Reset
// clears the payload while preserving capacity for the next write. Reusing one
// Frame across many writes avoids per-frame allocation on hot paths.
func ExampleFrame() {
	frame := moqt.NewFrame(64)

	_, _ = frame.Write([]byte("hello"))
	_, _ = frame.Write([]byte(" world"))
	fmt.Printf("%s\n", frame.Body())
	fmt.Println(frame.Len())

	frame.Reset() // payload gone, buffer capacity retained
	fmt.Println(frame.Len())

	// Output:
	// hello world
	// 11
	// 0
}
