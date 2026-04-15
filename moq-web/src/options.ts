import type { TrackMux } from "./track_mux.ts";

/**
 * Factory that creates a {@link WebTransport} instance for a MOQ session.
 *
 * The default factory calls `new WebTransport(url, options)`. Override this
 * to inject an alternative implementation that satisfies the {@link WebTransport}
 * interface (e.g. a WebSocket-backed shim, an in-memory fake for tests, etc.).
 * The returned object is automatically wrapped in a {@link WebTransportSession}
 * which adapts it to the internal stream API.
 *
 * @param url - The endpoint URL.
 * @param options - WebTransport options (may be ignored by custom factories).
 */
export type TransportFactory = (
	url: string | URL,
	options?: WebTransportOptions,
) => WebTransport;

/** Init object for {@link connect} — mirrors the `fetch(url, init?)` pattern. */
export interface ConnectInit {
	/** {@link TrackMux} for incoming track routing. Defaults to {@link DefaultTrackMux}. */
	mux?: TrackMux;
	/** Low-level WebTransport options forwarded to the transport factory. */
	transportOptions?: WebTransportOptions;
	/** Called when the server requests session migration via GOAWAY. */
	onGoaway?: (newSessionURI: string) => void;
	/**
	 * Custom transport factory.
	 * Defaults to a standard {@link WebTransport}-backed implementation.
	 * Override to inject alternative transports (WebSocket, in-memory, etc.).
	 */
	transportFactory?: TransportFactory;
}

// Back-compat aliases
/** @deprecated Use {@link ConnectInit}. */
export type ConnectOptions = ConnectInit;
/** @deprecated Use {@link ConnectInit}. */
export type MOQOptions = ConnectInit;
