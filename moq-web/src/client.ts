import { Session } from "./session.ts";
import type { ConnectInit } from "./options.ts";
import { openFirstReady } from "./internal/open_transport.ts";
import type { TransportOpener } from "./internal/open_transport.ts";
import { ALPN, openWebSocketTransport, transportCandidates } from "./transport.ts";

export { ALPN };

/**
 * How long {@link connect} waits for WebSocket before it falls back to
 * WebTransport, on WebKit. A network that drops the WebSocket's TCP port
 * without answering would otherwise hold the fallback up for as long as the
 * browser keeps trying.
 */
const webSocketFallbackTimeoutMs = 5000;

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
 * @example Force the WebSocket transport
 * ```ts
 * const session = await connect(url, { transport: "websocket" });
 * ```
 *
 * @example Custom transport (e.g. for testing)
 * ```ts
 * const session = await connect(url, {
 *   transportFactory: (u) => new MyTransport(u),
 * });
 * ```
 *
 * The transport is WebTransport where it works, and QMux over WebSocket
 * elsewhere: on WebKit, and where there is no `WebTransport`. On WebKit a
 * server that takes no WebSocket is reached over WebTransport instead. See
 * {@link transportCandidates}.
 *
 * The WebSocket is dialed at the same host and port as `url`, with `wss:`
 * for `https:`. A server that takes WebSocket elsewhere is reached with
 * {@link ConnectInit.webSocketURL}.
 *
 * @param url - MOQ server endpoint URL.
 * @param init - Connection init object (mux, onGoaway, transport, transportOptions, transportFactory).
 * @returns A ready-to-use {@link Session}.
 */
export async function connect(
	url: string | URL,
	init?: ConnectInit,
): Promise<Session> {
	const transportOptions: WebTransportOptions = {
		...DefaultWebTransportOptions,
		...(init?.transportOptions ?? {}),
	};

	const customFactory = init?.transportFactory;
	const openers: readonly TransportOpener[] = customFactory
		? [() => customFactory(url, transportOptions)]
		: transportCandidates(init?.transport).map((kind): TransportOpener =>
			kind === "websocket"
				? () => openWebSocketTransport(init?.webSocketURL ?? url)
				: () => new WebTransport(url, transportOptions)
		);

	try {
		// Only a transport that cannot be opened is passed over for the next.
		// Once one is open, the session is its own: a server that closes it,
		// as one that refuses the client does, is not asked again another way.
		const transport = await openFirstReady(openers, webSocketFallbackTimeoutMs);
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
