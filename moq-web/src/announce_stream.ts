import { EOFError } from "@okdaichi/golikejs/io";
import type { AnnounceInterestMessage } from "./internal/message/mod.ts";
import { AnnounceMessage } from "./internal/message/mod.ts";
import { ContextCancelledError, watchPromise, withCancelCause } from "@okdaichi/golikejs/context";
import type { CancelCauseFunc, Context } from "@okdaichi/golikejs/context";
import { Cond, Mutex } from "@okdaichi/golikejs/sync";
import type { TrackPrefix } from "./track_prefix.ts";
import { isValidPrefix, validateTrackPrefix } from "./track_prefix.ts";
import { validateBroadcastPath } from "./broadcast_path.ts";
import type { BroadcastPath } from "./broadcast_path.ts";
import { WebTransportStreamError } from "./internal/webtransport/error.ts";
import { Queue } from "./internal/queue.ts";
import { AnnounceError, AnnounceErrorCode } from "./error.ts";
import { Stream } from "./internal/webtransport/stream.ts";

type suffix = string;

/**
 * Writes announcements to a remote peer over an announce stream.
 *
 * Created on the publisher side to respond to an ANNOUNCE_INTEREST from the subscriber.
 * Call {@link init} with a set of initial announcements, then {@link send} for updates.
 */
export class AnnouncementWriter {
	#stream: Stream;
	readonly prefix: TrackPrefix;
	#announcements: Map<suffix, Announcement> = new Map();
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	#ready: Promise<void>;
	#resolveInit?: () => void;

	constructor(
		sessCtx: Context,
		stream: Stream,
		req: AnnounceInterestMessage,
	) {
		this.#stream = stream;
		this.prefix = validateTrackPrefix(req.prefix);

		// const ctx = watchPromise(sessCtx, reader.closed());
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);
		this.#ready = new Promise<void>((resolve) => {
			this.#resolveInit = resolve;
		});
	}

	/**
	 * Initialize the writer with a batch of announcements.
	 * Must be called exactly once before {@link send}.
	 * @param anns - Initial announcements to send.
	 */
	async init(anns: Announcement[]): Promise<Error | undefined> {
		// const onEndFuncs:Map<suffix, () => void> = new Map();
		for (const announcement of anns) {
			const path = announcement.broadcastPath;
			const active = announcement.isActive();

			if (!path.startsWith(this.prefix)) {
				return new Error(
					`Path ${path} does not start with prefix ${this.prefix}`,
				);
			}

			const suffix = path.substring(this.prefix.length);
			const old = this.#announcements.get(suffix);
			if (active) {
				if (old && old.isActive()) {
					return new Error(
						`[AnnouncementWriter] announcement for path ${this.prefix}${suffix} already exists`,
					);
				} else if (old && !old.isActive()) {
					// Delete the old announcement if it is inactive
					this.#announcements.delete(suffix);
				}

				this.#announcements.set(suffix, announcement);

				announcement.ended().then(async () => {
					// When the announcement ends, we remove it from the map
					this.#announcements.delete(suffix);
					const msg = new AnnounceMessage({ suffix, active: false });
					const err = await msg.encode(this.#stream.writable);
					if (err && err instanceof WebTransportStreamError) {
						return new AnnounceError(err.code, err.remote);
					}

					return err;
				}).catch(() => {});
			} else {
				if (!old || (old && !old.isActive())) {
					return new Error(
						`[AnnouncementWriter] announcement to end for path ${this.prefix}${suffix} is not active.`,
					);
				}

				// End the old active announcement
				old.end();
				this.#announcements.delete(suffix);
			}
		}

		// Send ACTIVE AnnounceMessage for each initial announcement
		for (const [sfx, announcement] of this.#announcements.entries()) {
			const msg = new AnnounceMessage({
				suffix: sfx,
				active: true,
				hopIDs: [...announcement.hopIDs],
			});
			const err = await msg.encode(this.#stream.writable);
			if (err) {
				return err;
			}
		}

		// Resolve the initialization promise
		this.#resolveInit?.();
		this.#resolveInit = undefined;

		return undefined;
	}

	/**
	 * Send a single announcement update after initialization.
	 * @param announcement - The announcement to add or end.
	 */
	async send(announcement: Announcement): Promise<Error | undefined> {
		await this.#ready; // Wait for initialization to complete

		const path = announcement.broadcastPath;
		const active = announcement.isActive();

		if (!path.startsWith(this.prefix)) {
			return new Error(
				`Path ${path} does not start with prefix ${this.prefix}`,
			);
		}

		const suffix = path.substring(this.prefix.length);
		const old = this.#announcements.get(suffix);
		if (active) {
			if (old && old.isActive()) {
				return new Error(
					`[AnnouncementWriter] announcement for path ${suffix} already exists`,
				);
			} else if (old && !old.isActive()) {
				// Delete the old announcement if it is inactive
				this.#announcements.delete(suffix);
			}

			const msg = new AnnounceMessage({ suffix, active });
			let err = await msg.encode(this.#stream.writable);
			if (err) {
				return err;
			}

			this.#announcements.set(suffix, announcement);

			announcement.ended().then(async () => {
				this.#announcements.delete(suffix);
				msg.active = false;
				err = await msg.encode(this.#stream.writable);
				if (err) {
					return err;
				}

				return undefined;
			}).catch(() => {});
		} else {
			if (!old || (old && !old.isActive())) {
				return new Error(
					`[AnnouncementWriter] announcement to end for path ${this.prefix}${suffix} is not active`,
				);
			}

			// End the old active announcement
			old.end();
			this.#announcements.delete(suffix);
		}

		return undefined;
	}

	/** Gracefully close the announce stream and end all active announcements. */
	async close(): Promise<void> {
		if (this.context.err()) {
			// If already closed, do nothing
			return;
		}
		this.#cancelFunc(undefined);
		await this.#stream.writable.close();
		// End all announcements
		for (const announcement of this.#announcements.values()) {
			announcement.end();
		}
		this.#announcements.clear();
		this.#resolveInit?.();
		this.#resolveInit = undefined;
	}

	/**
	 * Close the announce stream with an error code.
	 * @param code - The {@link AnnounceErrorCode} to send.
	 */
	async closeWithError(code: AnnounceErrorCode): Promise<void> {
		if (this.context.err()) {
			// If already closed, do nothing
			return;
		}

		const cause = new WebTransportStreamError(
			{ source: "stream", streamErrorCode: code },
			false,
		);
		this.#cancelFunc(cause);
		await this.#stream.writable.cancel(code);
		await this.#stream.readable.cancel(code);
		this.#announcements.clear();
		this.#resolveInit?.();
		this.#resolveInit = undefined;
	}
}

/**
 * Reads announcements from a remote peer over an announce stream.
 *
 * Created on the subscriber side after sending an ANNOUNCE_INTEREST.
 * Use {@link receive} in a loop to consume incoming announcements.
 */
export class AnnouncementReader {
	#stream: Stream;
	readonly prefix: string;
	#announcements: Map<string, Announcement> = new Map();
	#queue: Queue<Announcement> = new Queue();
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	#mu: Mutex = new Mutex();
	#cond: Cond = new Cond(this.#mu);

	constructor(
		sessCtx: Context,
		stream: Stream,
		announceInterest: AnnounceInterestMessage,
	) {
		this.#stream = stream;
		const prefix = announceInterest.prefix;
		if (!isValidPrefix(prefix)) {
			throw new Error(`[AnnouncementReader] invalid prefix: ${prefix}.`);
		}
		this.prefix = prefix;
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);

		// Start reading messages from the stream
		this.#readNext();
	}

	/**
	 * Wait for the next active announcement.
	 * @param signal - A promise that, when resolved, cancels the wait.
	 * @returns The next {@link Announcement}, or an Error.
	 */
	async receive(
		signal: Promise<void>,
	): Promise<[Announcement, undefined] | [undefined, Error]> {
		const ctx = watchPromise(this.context, signal);

		while (true) {
			const announcement = await this.#queue.dequeue();
			if (announcement === undefined) {
				return [undefined, new Error("Queue is closed and empty")];
			}

			if (announcement && announcement.isActive()) {
				return [announcement, undefined];
			}

			const err = ctx.err();
			if (err) {
				return [undefined, err];
			}

			// Wait for either context cancellation or a condition signal.
			// Using Promise.race here is safe because `cond.wait()` is implemented such that
			// it is a lightweight synchronization primitive and does not capture heavy resources.
			// Even if `cond.wait()` loses the race, it does not keep large memory references alive.
			const result = await Promise.race([
				ctx.done().then(() => ctx.err() ?? ContextCancelledError),
				this.#cond.wait(),
			]);

			if (result instanceof Error) {
				return [undefined, result];
			}
		}
	}

	#readNext(): void {
		const msg = new AnnounceMessage({});
		msg.decode(this.#stream.readable).then(async (err) => {
			if (err) {
				// EOFError and connection closed errors are expected during normal shutdown
				if (err instanceof EOFError) {
					return;
				}
				if (err instanceof WebTransportStreamError) {
					throw new AnnounceError(err.code, err.remote);
				}

				// Only log as error if context is still active (not shutting down)
				// and it's not a connection reset during shutdown
				if (
					!this.context.err() &&
					!(err.message?.includes("ConnectionReset") ||
						err.message?.includes("stream reset"))
				) {
					console.error(`moq: failed to read ANNOUNCE message: ${err}`);
				}
				return;
			}

			const old = this.#announcements.get(msg.suffix);

			if (msg.active) {
				if (old && old.isActive()) {
					await this.closeWithError(AnnounceErrorCode.DuplicatedAnnounce);

					return;
				} else if (old && !old.isActive()) {
					this.#announcements.delete(msg.suffix);
				}

				const fullPath = this.prefix + msg.suffix;
				const announcement = new Announcement(
					validateBroadcastPath(fullPath),
					this.context.done(),
					msg.hopIDs,
				);
				this.#announcements.set(msg.suffix, announcement);
				this.#queue.enqueue(announcement);
			} else {
				if (!old || (old && !old.isActive())) {
					await this.closeWithError(AnnounceErrorCode.DuplicatedAnnounce);

					return;
				}

				old.end();
				this.#announcements.delete(msg.suffix);
			}

			this.#cond.broadcast();

			// Check if context is cancelled before continuing the loop
			if (this.context.err()) {
				return;
			}

			queueMicrotask(() => this.#readNext());
		}).catch(() => {});
	}

	async close(): Promise<void> {
		if (this.context.err()) {
			// If already closed, do nothing
			return;
		}

		this.#cancelFunc(undefined);

		await this.#stream.writable.close();
		this.#announcements.clear();
		this.#queue.close();
	}

	async closeWithError(code: AnnounceErrorCode): Promise<void> {
		if (this.context.err()) {
			// If already closed, do nothing
			return;
		}
		const cause = new WebTransportStreamError(
			{ source: "stream", streamErrorCode: code },
			false,
		);
		this.#cancelFunc(cause);
		await this.#stream.writable.cancel(code);
		await this.#stream.readable.cancel(code);
		this.#announcements.clear();
		this.#queue.close();
	}
}

/**
 * Represents a single broadcast announcement that is active or ended.
 *
 * An announcement carries a {@link BroadcastPath} and transitions from active to ended
 * when {@link end} is called or the parent signal resolves.
 */
export class Announcement {
	/** The broadcast path this announcement refers to. */
	readonly broadcastPath: BroadcastPath;
	/** Hop IDs this announcement has traversed. */
	readonly hopIDs: number[];
	#done: Promise<void>;
	#signalFunc: () => void;
	#active: boolean = true;

	constructor(path: string, signal: Promise<void>, hopIDs: number[] = []) {
		this.broadcastPath = validateBroadcastPath(path);
		this.hopIDs = hopIDs;

		let resolveFunc: () => void;
		this.#done = new Promise<void>((resolve) => {
			resolveFunc = resolve;
		});

		this.#signalFunc = () => resolveFunc();

		// Cancel when the signal is done
		signal.then(() => {
			this.end();
		}).catch(() => {});
	}

	/** Mark this announcement as ended. Idempotent. */
	end(): void {
		if (!this.#active) {
			return;
		}
		this.#active = false;
		this.#signalFunc();
	}

	/** Returns `true` if the announcement has not yet ended. */
	isActive(): boolean {
		return this.#active;
	}

	/** A promise that resolves when the announcement ends. */
	ended(): Promise<void> {
		return this.#done;
	}

	/**
	 * Register a callback to run when the announcement ends.
	 * @param fn - Callback to invoke.
	 * @returns A stop function; returns `false` if the callback already fired.
	 */
	afterFunc(fn: () => void): () => boolean {
		let executed = false;
		this.#done.then(() => {
			if (executed) return;
			executed = true;
			fn();
		}).catch(() => {});

		return () => {
			if (executed) {
				return false;
			}
			executed = true;
			return !executed;
		};
	}
}
