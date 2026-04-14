import { Session } from "./session.ts";
import type { MOQOptions } from "./options.ts";
import { DefaultTrackMux, TrackMux } from "./track_mux.ts";
import { WebTransportSession } from "./internal/webtransport/mod.ts";

/** ALPN protocol identifier for MOQ Lite draft-03. */
export const ALPN = "moq-lite-03";

const DefaultWebTransportOptions: WebTransportOptions = {
	allowPooling: false,
	congestionControl: "low-latency",
	requireUnreliable: true,
	// deno-lint-ignore no-explicit-any
	...(({ protocols: [ALPN] }) as any),
};

const DefaultMOQOptions: MOQOptions = {
	reconnect: false, // TODO: Implement reconnect logic
	transportOptions: DefaultWebTransportOptions,
};

/**
 * High-level MOQ client that creates {@link Session}s over WebTransport.
 *
 * @example
 * ```ts
 * const client = new Client();
 * const session = await client.dial("https://localhost:4443/moq");
 * ```
 */
export class Client {
	#sessions?: Set<Session> = new Set();
	readonly options: MOQOptions;

	/**
	 * Create a new Client.
	 * The provided options are shallow-merged with safe defaults so the
	 * shared default objects aren't accidentally mutated.
	 */
	constructor(options?: MOQOptions) {
		this.options = {
			reconnect: options?.reconnect ?? DefaultMOQOptions.reconnect,
			transportOptions: {
				...DefaultWebTransportOptions,
				...(options?.transportOptions ?? {}),
			},
		};
	}

	/**
	 * Open a new MOQ session to the given URL.
	 * @param url - WebTransport endpoint URL.
	 * @param mux - Optional {@link TrackMux} for incoming track routing. Defaults to {@link DefaultTrackMux}.
	 * @returns A ready-to-use {@link Session}.
	 */
	async dial(
		url: string | URL,
		mux: TrackMux = DefaultTrackMux,
	): Promise<Session> {
		if (this.#sessions === undefined) {
			return Promise.reject(new Error("Client is closed"));
		}

		// Normalize URL to string (WebTransport accepts a USVString).
		// const endpoint = typeof url === "string" ? url : String(url);

		try {
			const webtransport = new WebTransportSession(
				url,
				this.options.transportOptions,
			);
			const session = new Session({
				transport: webtransport,
				mux,
			});
			await session.ready;
			this.#sessions.add(session);
			return session;
		} catch (err) {
			return Promise.reject(new Error(`failed to create WebTransport: ${err}`));
		}
	}

	/** Gracefully close all active sessions. */
	async close(): Promise<void> {
		if (this.#sessions === undefined) {
			return Promise.resolve();
		}

		await Promise.allSettled(
			Array.from(this.#sessions).map((session) => session.close()),
		);
		// Mark client as closed so future dials fail fast.
		this.#sessions = undefined;
	}

	/** Abort all active sessions immediately with an error code. */
	async abort(): Promise<void> {
		if (this.#sessions === undefined) {
			return;
		}

		// Try to close sessions with an error to indicate abort semantics.
		await Promise.allSettled(
			Array.from(this.#sessions).map((session) =>
				session.closeWithError(1, "client aborted")
			),
		);

		// Mark closed
		this.#sessions = undefined;
	}
}
