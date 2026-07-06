import { SubscribeErrorCode } from "./error.ts";
import type { Info } from "./info.ts";
import type { TrackHandler, TrackInfoProvider } from "./track_mux.ts";
import type { TrackWriter } from "./track_writer.ts";

/**
 * Per-track handler aggregation for a single broadcast.
 *
 * Registers named {@link TrackHandler}s and dispatches incoming
 * subscriptions by track name.
 */
export class Broadcast implements TrackHandler, TrackInfoProvider {
	#trackHandlers = new Map<string, TrackHandlerEntry>();

	/**
	 * Register a handler with default publisher properties. Use
	 * {@link registerWithInfo} to declare explicit properties.
	 */
	async register(name: string, handler: TrackHandler): Promise<void> {
		await this.registerWithInfo(name, undefined, handler);
	}

	/**
	 * Register a handler along with the track's immutable publisher
	 * properties, served to subscribers via TRACK_INFO. A zero timescale is
	 * treated as the default (1000).
	 */
	async registerWithInfo(
		name: string,
		info: Info | undefined,
		handler: TrackHandler,
	): Promise<void> {
		if (name === "") {
			throw new Error("moq: track name is required");
		}
		if (handler === undefined || handler === null) {
			throw new Error("moq: track handler cannot be nil");
		}

		const entry = new TrackHandlerEntry(handler, info);
		const previous = this.#trackHandlers.get(name);
		this.#trackHandlers.set(name, entry);

		if (previous) {
			await previous.close();
		}
	}

	/** Implements {@link TrackInfoProvider} for registered tracks. */
	trackInfo(name: string): Info | undefined {
		const entry = this.#trackHandlers.get(name);
		if (!entry) {
			return undefined;
		}
		return entry.info ?? { priority: 0, ordered: false, maxLatency: 0, timescale: 0 };
	}

	async remove(name: string): Promise<boolean> {
		if (name === "") {
			return false;
		}

		const entry = this.#trackHandlers.get(name);
		if (!entry) {
			return false;
		}

		this.#trackHandlers.delete(name);
		await entry.close();
		return true;
	}

	async close(): Promise<void> {
		const entries = [...this.#trackHandlers.values()];
		this.#trackHandlers.clear();
		await Promise.allSettled(entries.map((entry) => entry.close()));
	}

	handler(name: string): TrackHandler {
		if (name === "") {
			return NotFoundTrackHandler;
		}
		return this.#trackHandlers.get(name) ?? NotFoundTrackHandler;
	}

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		await this.handler(trackWriter.trackName).serveTrack(trackWriter);
	}
}

/**
 * Default handler that closes the track with {@link SubscribeErrorCode.TrackNotFound}.
 */
export async function NotFound(trackWriter: TrackWriter): Promise<void> {
	await trackWriter.closeWithError(SubscribeErrorCode.TrackNotFound);
}

class TrackHandlerEntry implements TrackHandler {
	#handler: TrackHandler;
	readonly info?: Info;
	#active = new Set<TrackWriter>();
	#stopped = false;

	constructor(handler: TrackHandler, info?: Info) {
		this.#handler = handler;
		this.info = info;
	}

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		if (!this.#trackStarted(trackWriter)) {
			await NotFoundTrackHandler.serveTrack(trackWriter);
			return;
		}

		try {
			await this.#handler.serveTrack(trackWriter);
		} finally {
			this.#trackEnded(trackWriter);
		}
	}

	async close(): Promise<void> {
		if (this.#stopped) {
			return;
		}
		this.#stopped = true;
		const active = [...this.#active];
		this.#active.clear();
		await Promise.allSettled(active.map((trackWriter) => trackWriter.close()));
	}

	#trackStarted(trackWriter: TrackWriter): boolean {
		if (this.#stopped) {
			return false;
		}
		this.#active.add(trackWriter);
		return true;
	}

	#trackEnded(trackWriter: TrackWriter): void {
		this.#active.delete(trackWriter);
	}
}

/** A {@link TrackHandler} that always responds with "track not found". */
export const NotFoundTrackHandler: TrackHandler = {
	serveTrack: NotFound,
};
