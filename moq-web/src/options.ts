import type { TrackMux } from "./track_mux.ts";
import type { FetchHandler } from "./fetch.ts";

/** Default poll interval for the publisher-side probe detection loop (ms). */
export const defaultProbeIntervalMs = 100;
/** Default maximum age before a probe update is re-sent unconditionally (ms). */
export const defaultProbeMaxAgeMs = 10_000;
/** Default minimum relative bitrate change required to trigger an early re-send. */
export const defaultProbeMaxDelta = 0.1;

/**
 * MOQ session tuning options — mirrors Go's {@link https://pkg.go.dev/github.com/qumo-dev/gomoqt/moqt#Config moqt.Config}.
 *
 * Contains only low-level tuning parameters. Per-session arguments such as
 * `mux`, `fetchHandler`, and `onGoaway` are passed as direct fields on
 * {@link SessionInit} or {@link ConnectInit}.
 */
export interface MoqOptions {
	/** Poll interval for publisher-side bitrate detection (ms). Defaults to {@link defaultProbeIntervalMs}. */
	probeIntervalMs?: number;
	/** Maximum age before a probe update is re-sent unconditionally (ms). Defaults to {@link defaultProbeMaxAgeMs}. */
	probeMaxAgeMs?: number;
	/** Minimum relative bitrate change required to trigger an early re-send. Defaults to {@link defaultProbeMaxDelta}. */
	probeMaxDelta?: number;
}

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
	/** Handler invoked for incoming fetch requests. */
	fetchHandler?: FetchHandler;
	/** Called when the server requests session migration via GOAWAY. */
	onGoaway?: (newSessionURI: string) => void;
	/** Low-level MOQ tuning options (probe intervals, thresholds, etc.). */
	options?: MoqOptions;
	/** Low-level WebTransport options forwarded to the transport factory. */
	transportOptions?: WebTransportOptions;
	/**
	 * Custom transport factory.
	 * Defaults to a standard {@link WebTransport}-backed implementation.
	 * Override to inject alternative transports (WebSocket, in-memory, etc.).
	 */
	transportFactory?: TransportFactory;
}
