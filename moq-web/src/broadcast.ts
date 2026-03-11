import { SubscribeErrorCode } from "./error.ts";
import type { TrackHandler } from "./track_mux.ts";
import type { TrackWriter } from "./track_writer.ts";

export class Broadcast implements TrackHandler {
	#trackHandlers = new Map<string, TrackHandlerEntry>();

	async register(name: string, handler: TrackHandler): Promise<void> {
		if (name === "") {
			throw new Error("moq: track name is required");
		}
		if (handler === undefined || handler === null) {
			throw new Error("moq: track handler cannot be nil");
		}

		const entry = new TrackHandlerEntry(handler);
		const previous = this.#trackHandlers.get(name);
		this.#trackHandlers.set(name, entry);

		if (previous) {
			await previous.close();
		}
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

export async function NotFound(trackWriter: TrackWriter): Promise<void> {
	await trackWriter.closeWithError(SubscribeErrorCode.TrackNotFound);
}

class TrackHandlerEntry implements TrackHandler {
	#handler: TrackHandler;
	#active = new Set<TrackWriter>();
	#stopped = false;

	constructor(handler: TrackHandler) {
		this.#handler = handler;
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

export const NotFoundTrackHandler: TrackHandler = {
	serveTrack: NotFound,
};
