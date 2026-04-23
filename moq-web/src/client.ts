import { Session } from "./session.ts";
import type { ConnectInit } from "./options.ts";
import { WebTransportSession } from "./internal/webtransport/mod.ts";

/** ALPN protocol identifier for MOQ Lite draft-04. */
export const ALPN = "moq-lite-04";

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
 * @example Custom transport (e.g. for testing)
 * ```ts
 * const session = await connect(url, {
 *   transportFactory: (u) => new MyWebSocketTransport(u),
 * });
 * ```
 *
 * @param url - MOQ server endpoint URL.
 * @param init - Connection init object (mux, onGoaway, transportOptions, transportFactory).
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

	const factory = init?.transportFactory ??
		((u: string | URL, o?: WebTransportOptions) => new WebTransport(u, o));

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
