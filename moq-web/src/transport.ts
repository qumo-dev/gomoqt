import QMuxSession from "@moq/qmux";

/** ALPN protocol identifier for MOQ Lite draft-05. */
export const ALPN = "moq-lite-05";

/**
 * The QMux draft spoken over WebSocket. The WebSocket subprotocol names it
 * with the application protocol: `qmux-02.moq-lite-05`.
 */
export const QMUX_VERSION = "qmux-02";

/**
 * A transport {@link connect} can use.
 *
 * - `"webtransport"` is WebTransport, over QUIC. It is the default.
 * - `"websocket"` is QMux over WebSocket: the same streams over one TCP
 *   connection, for a browser whose WebTransport does not work. A lost
 *   segment delays every stream, and there are no datagrams.
 *
 * The application chooses. See {@link isWebKit} for the browsers that need
 * `"websocket"`.
 */
export type TransportKind = "webtransport" | "websocket";

/**
 * Reports whether a user agent string is that of a WebKit browser: Safari,
 * and every browser on iOS, where Chrome, Firefox and Edge are WebKit too.
 *
 * WebKit's `WebTransport` never raises the stream and data limits it
 * grants, so a session stalls after about 7,600 streams or 16 MB
 * (https://bugs.webkit.org/show_bug.cgi?id=319818). The stall cannot be
 * detected after connecting, as the session looks healthy until then, so an
 * application that serves these browsers chooses the transport up front:
 *
 * ```ts
 * const transport = isWebKit(navigator.userAgent) ? "websocket" : "webtransport";
 * const session = await connect(url, { transport });
 * ```
 *
 * Chromium browsers also say `AppleWebKit`, so they are told apart by the
 * `Chrome/`, `Chromium/`, `Edg/` and `OPR/` tokens, which no WebKit browser
 * sends.
 *
 * It looks at the engine, not at its version: the bug has no fix to date.
 * An application that wants WebTransport back on a WebKit that has one
 * adds its own version bound to this check.
 */
export function isWebKit(userAgent: string): boolean {
	return /AppleWebKit\//.test(userAgent) && !/(Chrome|Chromium|Edg|OPR)\//.test(userAgent);
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
