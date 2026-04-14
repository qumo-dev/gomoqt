import type { BroadcastPath } from "./broadcast_path.ts";
import type { GroupSequence, TrackName, TrackPriority } from "./alias.ts";
import type { GroupWriter } from "./group_stream.ts";

/** Options for constructing a {@link FetchRequest}. */
export interface FetchRequestInit {
	/** Target broadcast path. */
	broadcastPath: BroadcastPath;
	/** Track name within the broadcast. */
	trackName: TrackName;
	/** Priority of this fetch request. */
	priority: TrackPriority;
	/** Group sequence to fetch. */
	groupSequence: GroupSequence;
	/** Optional cancellation signal. */
	done?: Promise<void>;
}

/**
 * Represents a request to fetch a single group from a track.
 *
 * Immutable — use {@link withDone} or {@link clone} to derive a new
 * request with a different cancellation signal.
 */
export class FetchRequest {
	readonly broadcastPath: BroadcastPath;
	readonly trackName: TrackName;
	readonly priority: TrackPriority;
	readonly groupSequence: GroupSequence;
	#done: Promise<void>;

	constructor(init: FetchRequestInit) {
		this.broadcastPath = init.broadcastPath;
		this.trackName = init.trackName;
		this.priority = init.priority;
		this.groupSequence = init.groupSequence;
		this.#done = init.done ?? new Promise(() => {});
	}

	done(): Promise<void> {
		return this.#done;
	}

	withDone(done: Promise<void>): FetchRequest {
		return new FetchRequest({
			broadcastPath: this.broadcastPath,
			trackName: this.trackName,
			priority: this.priority,
			groupSequence: this.groupSequence,
			done,
		});
	}

	clone(done: Promise<void>): FetchRequest {
		return new FetchRequest({
			broadcastPath: this.broadcastPath,
			trackName: this.trackName,
			priority: this.priority,
			groupSequence: this.groupSequence,
			done,
		});
	}
}

/** Handler invoked for incoming fetch requests on the publisher side. */
export interface FetchHandler {
	/**
	 * Serve a single-group fetch.
	 * @param w - The {@link GroupWriter} to write the response group into.
	 * @param r - The incoming {@link FetchRequest}.
	 */
	serveFetch(w: GroupWriter, r: FetchRequest): void | Promise<void>;
}
