import { Session } from "./session.ts";
import type { ConnectInit } from "./options.ts";
import { WebTransportSession } from "./internal/webtransport/mod.ts";
import type { TransportFactory } from "./options.ts";
import { ALPN, openWebSocketTransport, transportCandidates } from "./transport.ts";

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

	const factories: readonly TransportFactory[] = init?.transportFactory
		? [init.transportFactory]
		: transportCandidates(init?.transport).map((kind): TransportFactory =>
			kind === "websocket"
				? (u) => openWebSocketTransport(u)
				: (u, o) => new WebTransport(u, o)
		);

	// The last transport's failure is the one reported: an earlier one was
	// only tried first.
	let failure: unknown;
	for (const factory of factories) {
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
			failure = err;
		}
	}
	throw new Error(`failed to connect: ${failure}`);
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
