// Package moqt provides MOQ Lite client and server primitives for Media over QUIC.
// It follows the MOQ Lite specification (draft-lcurley-moq-lite-03) and supports both
// native QUIC and WebTransport transports.
//
// # Clients
//
// Use a Dialer to create client sessions from a URL. The Dial method selects the
// transport from the URL scheme: https for WebTransport and moqt for native QUIC.
// DialWebTransport and DialQUIC can also be used directly when you already know the
// target transport.
//
// Native QUIC client example:
//
//	client := &moqt.Dialer{}
//	sess, err := client.Dial(ctx, "moqt://example.com:4433/live", mux)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer sess.Close()
//
// WebTransport client example:
//
//	client := &moqt.Dialer{}
//	sess, err := client.Dial(ctx, "https://example.com:4433/live", mux)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer sess.Close()
//
// # Servers
//
// Use Server or ListenAndServe to accept incoming native QUIC connections. Server
// routes those sessions through Handler. For HTTP upgrade-based WebTransport
// endpoints, WebTransportHandler upgrades the request, creates a session, and
// dispatches it to Handler. TrackMux can be used to route announcements and
// subscriptions for published tracks.
//
// Native QUIC server example:
//
//	server := &moqt.Server{
//		Addr:      ":4433",
//		TLSConfig: tlsConfig,
//		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
//			defer sess.Close()
//			// handle native QUIC session
//		}),
//		TrackMux: moqt.NewTrackMux(),
//	}
//	if err := server.ListenAndServe(); err != nil {
//		log.Fatal(err)
//	}
//
// WebTransport server example:
//
//	handler := &moqt.WebTransportHandler{
//		TrackMux: moqt.NewTrackMux(),
//		Handler: moqt.HandleFunc(func(sess *moqt.Session) {
//			defer sess.Close()
//			// handle WebTransport session
//		}),
//	}
//	if err := http.ListenAndServe(":8080", handler); err != nil {
//		log.Fatal(err)
//	}
//
// # Transport customization
//
// Custom transport hooks can be supplied through Dialer.DialQUICFunc,
// Dialer.DialWebTransportFunc, Server.ListenFunc, Server.WebTransportServer, and
// WebTransportHandler.UpgradeFunc.
package moqt
