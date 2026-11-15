import { Session } from "./session.ts";
import type { ConnectInit, TransportFactory } from "./options.ts";
import { WebTransportSession } from "./internal/webtransport/mod.ts";
import { ALPN, openWebSocketTransport } from "./transport.ts";
import { tryInOrder } from "./internal/try_in_order.ts";

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
	return await open(url, init, transportFactoryFor(init));
}

/**
 * The transport factory `init` asks for: its own, or WebTransport, or
 * WebSocket at {@link ConnectInit.webSocketURL} or the URL dialed.
 *
 * @throws TypeError if `init.transport` is neither `"webtransport"` nor `"websocket"`.
 */
function transportFactoryFor(init?: ConnectInit): TransportFactory {
	const kind: unknown = init?.transport ?? "webtransport";
	if (kind !== "webtransport" && kind !== "websocket") {
		// A value from JavaScript or from configuration: opening WebTransport
		// for a misspelt "websocket" would be the wrong transport, silently.
		throw new TypeError(`transport must be "webtransport" or "websocket", not ${String(kind)}`);
	}
	return init?.transportFactory ??
		(kind === "websocket"
			? (u: string | URL) => openWebSocketTransport(init?.webSocketURL ?? u)
			: (u: string | URL, o?: WebTransportOptions) => new WebTransport(u, o));
}

/**
 * Opens a session to `url` with `factory`'s transport, as {@link connect}
 * does. It fails as soon as `signal` aborts, without waiting for the
 * transport. A session that fails or is abandoned is closed, with its
 * transport, so a failed open leaves nothing open.
 */
async function open(
	url: string | URL,
	init: ConnectInit | undefined,
	factory: TransportFactory,
	signal?: AbortSignal,
): Promise<Session> {
	const transportOptions: WebTransportOptions = {
		...DefaultWebTransportOptions,
		...(init?.transportOptions ?? {}),
	};

	let session: Session | undefined;
	try {
		signal?.throwIfAborted();
		session = new Session({
			transport: new WebTransportSession(factory(url, transportOptions)),
			mux: init?.mux,
			fetchHandler: init?.fetchHandler,
			onGoaway: init?.onGoaway,
			checkRedirect: init?.checkRedirect,
			options: init?.options,
		});
		await (signal === undefined
			? session.ready
			: Promise.race([session.ready, aborted(signal)]));
		signal?.throwIfAborted();
		return session;
	} catch (err) {
		// Not awaited: a transport that is stuck would hold up the failure
		// it is being closed for. close() settles its own errors.
		session?.close().catch(() => {});
		throw new Error(`failed to connect: ${err}`, { cause: err });
	}
}

/** A promise that rejects with `signal`'s reason once it aborts. */
function aborted(signal: AbortSignal): Promise<never> {
	return new Promise((_, reject) => {
		if (signal.aborted) {
			reject(signal.reason);
			return;
		}
		signal.addEventListener("abort", () => reject(signal.reason), { once: true });
	});
}

/** How long {@link connectAny} gives one URL by default, in milliseconds. */
export const DEFAULT_ATTEMPT_TIMEOUT = 5000;

/** Init object for {@link connectAny}. */
export interface ConnectAnyInit extends Omit<ConnectInit, "webSocketURL"> {
	/**
	 * How long one URL may take to connect, in milliseconds, before the next
	 * is tried. Defaults to {@link DEFAULT_ATTEMPT_TIMEOUT}; zero or less
	 * means no limit.
	 */
	attemptTimeout?: number;
	/**
	 * Where to dial the WebSocket transport for each URL, for servers that
	 * take WebSocket at another host, port or path than WebTransport. Called
	 * with the URL being tried. Defaults to that URL, with `wss:` for
	 * `https:`.
	 */
	webSocketURL?: (url: string | URL) => string | URL;
	/** Stops trying: the attempt in progress is closed and nothing more is tried. */
	signal?: AbortSignal;
}

/** The session {@link connectAny} opened, and which URL it opened it to. */
export interface ConnectedSession {
	/** The ready session. */
	readonly session: Session;
	/** The URL the session was opened to, as given. */
	readonly url: string | URL;
	/** The URL's place in the list given to {@link connectAny}. */
	readonly index: number;
}

/**
 * Open a MOQ session to the first of `urls` that takes it.
 *
 * The URLs are tried one at a time, in order, each with {@link connect}'s
 * behaviour and `init`; the first session to become ready is returned. A URL
 * that fails or takes longer than {@link ConnectAnyInit.attemptTimeout} is
 * given up on, its transport closed, and the next one is tried.
 *
 * Use it with a list of relays in preference order, such as a placement
 * service's answer: a relay that is full refuses new sessions, so the client
 * lands on the next one instead of failing. A browser does not show why a
 * WebTransport or WebSocket connection was refused, so every failure moves
 * on, whatever its cause.
 *
 * With `transport: "websocket"`, each URL's WebSocket is dialed where
 * {@link ConnectAnyInit.webSocketURL} says for that URL, or at that URL's
 * host and port.
 *
 * @example
 * ```ts
 * const { session, url } = await connectAny(relays.map((r) => r.url));
 * ```
 *
 * @param urls - MOQ server endpoint URLs, most preferred first.
 * @param init - Options for each attempt, plus the per-attempt timeout and an abort signal.
 * @returns The ready session and the URL it was opened to.
 * @throws TypeError when `urls` is empty, `init.transport` is unknown, or
 * `init.webSocketURL` is not a function.
 * @throws AggregateError of every attempt's error, in order, when no URL took the session.
 * @throws `init.signal`'s reason when it aborts first.
 */
export async function connectAny(
	urls: readonly (string | URL)[],
	init?: ConnectAnyInit,
): Promise<ConnectedSession> {
	const { attemptTimeout, signal, webSocketURL, ...connectInit } = init ?? {};
	const ws: unknown = webSocketURL;
	if (ws !== undefined && typeof ws !== "function") {
		// connect's single URL, passed from JavaScript, would send every
		// attempt to one server.
		throw new TypeError("webSocketURL must be a function of the URL being tried");
	}
	// Every URL's init and factory are worked out before any is dialed, so a
	// mistake in them, such as an unknown transport or a webSocketURL that
	// throws, comes back as itself and not as one failed relay after another.
	const candidates = urls.map((url) => {
		const attemptInit: ConnectInit = { ...connectInit, webSocketURL: webSocketURL?.(url) };
		return { url, init: attemptInit, factory: transportFactoryFor(attemptInit) };
	});
	const tried = await tryInOrder(
		candidates,
		(c, attemptSignal) => open(c.url, c.init, c.factory, attemptSignal),
		{ attemptTimeout: attemptTimeout ?? DEFAULT_ATTEMPT_TIMEOUT, signal },
	);
	return { session: tried.value, url: tried.candidate.url, index: tried.index };
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
