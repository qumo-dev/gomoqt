import QMuxSession from "@moq/qmux";

/** ALPN protocol identifier for MOQ Lite draft-05. */
export const ALPN = "moq-lite-05";

/**
 * The QMux draft spoken over WebSocket. The WebSocket subprotocol names it
 * with the application protocol: `qmux-02.moq-lite-05`.
 */
export const QMUX_VERSION = "qmux-02";

/**
 * Which transport {@link connect} uses.
 *
 * - `"auto"` picks WebTransport where it works, and WebSocket elsewhere. On
 *   WebKit it falls back to WebTransport when the server takes no WebSocket.
 * - `"webtransport"` and `"websocket"` force one, for testing.
 */
export type TransportKind = "auto" | "webtransport" | "websocket";

/** What {@link selectTransport} decides from. Defaults to the running environment. */
export interface TransportEnvironment {
	/** The browser's user agent string, or `undefined` outside a browser. */
	userAgent?: string;
	/** Whether a `WebTransport` constructor exists. */
	hasWebTransport: boolean;
}

function currentEnvironment(): TransportEnvironment {
	return {
		userAgent: globalThis.navigator?.userAgent,
		hasWebTransport: typeof globalThis.WebTransport === "function",
	};
}

/**
 * Reports whether a user agent string is that of a WebKit browser: Safari,
 * and every browser on iOS, where Chrome, Firefox and Edge are WebKit too.
 *
 * Chromium browsers also say `AppleWebKit`, so they are told apart by the
 * `Chrome/`, `Chromium/` and `Edg/` tokens, which no WebKit browser sends.
 */
export function isWebKit(userAgent: string): boolean {
	return /AppleWebKit\//.test(userAgent) && !/(Chrome|Chromium|Edg|OPR)\//.test(userAgent);
}

/**
 * Resolves a {@link TransportKind} to the transports to try, in order.
 *
 * WebKit is sent to WebSocket first even though it has a `WebTransport`: its
 * implementation never raises the stream and data limits it grants, so a
 * session stalls after about 7,600 streams or 16 MB
 * (https://bugs.webkit.org/show_bug.cgi?id=319818). The stall cannot be
 * detected after connecting, as the session looks healthy until then.
 *
 * WebTransport stays as the second choice there, for a server that takes no
 * WebSocket: a session that will stall is better than none.
 */
export function transportCandidates(
	kind: TransportKind = "auto",
	env: TransportEnvironment = currentEnvironment(),
): readonly ("webtransport" | "websocket")[] {
	if (kind !== "auto") {
		return [kind];
	}
	if (!env.hasWebTransport) {
		return ["websocket"];
	}
	if (env.userAgent !== undefined && isWebKit(env.userAgent)) {
		return ["websocket", "webtransport"];
	}
	return ["webtransport"];
}

/**
 * Resolves a {@link TransportKind} to the transport {@link connect} tries
 * first. See {@link transportCandidates}.
 */
export function selectTransport(
	kind: TransportKind = "auto",
	env: TransportEnvironment = currentEnvironment(),
): "webtransport" | "websocket" {
	return transportCandidates(kind, env)[0] ?? "webtransport";
}

/**
 * Opens a QMux session over WebSocket and presents it as a `WebTransport`.
 * An `https:` URL is dialed as `wss:`. The server must select the
 * subprotocol, or the connection fails: without it neither side knows what
 * the other speaks.
 */
export function openWebSocketTransport(url: string | URL): WebTransport {
	return new QMuxSession(url, {
		protocols: [ALPN],
		versions: { [ALPN]: QMUX_VERSION },
		requireProtocol: true,
	});
}
