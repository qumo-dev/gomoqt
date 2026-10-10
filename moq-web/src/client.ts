import { Session } from "./session.ts";
import type { ConnectInit } from "./options.ts";
import { WebTransportSession } from "./internal/webtransport/mod.ts";
import { ALPN, openWebSocketTransport } from "./transport.ts";

export { ALPN };

const DefaultWebTransportOptions: WebTransportOptions = {
	allowPooling: false,
	congestionControl: "low-latency",
	requireUnreliable: true,
	// deno-lint-ignore no-explicit-any
	...(({ protocols: [ALPN] }) as any),
};

/**
 * Open a new MOQ session to the given URL.
 *
 * @example
 * ```ts
 * const session = await connect("https://localhost:4443/moq");
 * // ... use session ...
 * await session.closeWithError(0, "done");
 * ```
 *
 * @example With options
 * ```ts
 * const session = await connect(url, {
 *   mux,
 *   onGoaway: (uri) => console.log("migrate to", uri),
 * });
 * ```
 *
 * @example Over WebSocket, for a browser whose WebTransport does not work
 * ```ts
 * const transport = isWebKit(navigator.userAgent) ? "websocket" : "webtransport";
 * const session = await connect(url, { transport });
 * ```
 *
 * @example Custom transport (e.g. for testing)
 * ```ts
 * const session = await connect(url, {
 *   transportFactory: (u) => new MyTransport(u),
 * });
 * ```
 *
 * The transport is WebTransport unless {@link ConnectInit.transport} says
 * `"websocket"`. The application chooses, and a transport that cannot be
 * opened fails the connection: nothing is chosen or switched for it. See
 * {@link isWebKit} for the browsers that need WebSocket.
 *
 * The WebSocket is dialed at the same host and port as `url`, with `wss:`
 * for `https:`. A server that takes WebSocket elsewhere is reached with
 * {@link ConnectInit.webSocketURL}.
 *
 * @param url - MOQ server endpoint URL.
 * @param init - Connection init object (mux, onGoaway, transport, transportOptions, transportFactory).
 * @returns A ready-to-use {@link Session}.
 * @throws TypeError if `init.transport` is neither `"webtransport"` nor `"websocket"`.
 */
export async function connect(
	url: string | URL,
	init?: ConnectInit,
): Promise<Session> {
	const transportOptions: WebTransportOptions = {
		...DefaultWebTransportOptions,
		...(init?.transportOptions ?? {}),
	};

	const kind: unknown = init?.transport ?? "webtransport";
	if (kind !== "webtransport" && kind !== "websocket") {
		// A value from JavaScript or from configuration: opening WebTransport
		// for a misspelt "websocket" would be the wrong transport, silently.
		throw new TypeError(`transport must be "webtransport" or "websocket", not ${String(kind)}`);
	}
	const factory = init?.transportFactory ??
		(kind === "websocket"
			? () => openWebSocketTransport(init?.webSocketURL ?? url)
			: (u: string | URL, o?: WebTransportOptions) => new WebTransport(u, o));

	try {
		const transport = new WebTransportSession(factory(url, transportOptions));
		const session = new Session({
			transport,
			mux: init?.mux,
			fetchHandler: init?.fetchHandler,
			onGoaway: init?.onGoaway,
			options: init?.options,
		});
		await session.ready;
		return session;
	} catch (err) {
		throw new Error(`failed to connect: ${err}`);
	}
}

// Back-compat shim — kept so existing code compiled against the old Client
// class still works. Deprecated: call {@link connect} directly.
/** @deprecated Use {@link connect} instead. */
export class Client {
	#init: ConnectInit;

	constructor(init?: ConnectInit) {
		this.#init = { ...init };
	}

	/** @deprecated Use {@link connect} instead. */
	dial(url: string | URL): Promise<Session> {
		return connect(url, this.#init);
	}
}
