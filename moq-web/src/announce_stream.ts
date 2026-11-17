import { EOFError } from "@okdaichi/golikejs/io";
import type { AnnounceRequestMessage } from "./internal/message/mod.ts";
import {
	AnnounceDecodeError,
	AnnounceEndMessage,
	AnnounceMessageTypes,
	AnnounceOkMessage,
	AnnounceStartMessage,
	AnnounceUpdateMessage,
	DEFAULT_COST,
	duplicateHopID,
	readVarint,
} from "./internal/message/mod.ts";
import { watchPromise, withCancelCause } from "@okdaichi/golikejs/context";
import type { CancelCauseFunc, Context } from "@okdaichi/golikejs/context";
import type { TrackPrefix } from "./track_prefix.ts";
import { isValidPrefix, validateTrackPrefix } from "./track_prefix.ts";
import { validateBroadcastPath } from "./broadcast_path.ts";
import type { BroadcastPath } from "./broadcast_path.ts";
import { WebTransportStreamError } from "./internal/webtransport/error.ts";
import { Queue } from "./internal/queue.ts";
import { AnnounceError, AnnounceErrorCode } from "./error.ts";
import { Stream } from "./internal/webtransport/stream.ts";

type suffix = string;

/** An advertisement's routing metadata (moq-lite-06 §7.6). */
interface Route {
	readonly hopIDs: readonly number[];
	readonly warmCost: number;
	readonly coldCost: number;
}

/**
 * The routing metadata of each announcement, kept beside it so an
 * ANNOUNCE_UPDATE can replace it in place. A local announcement has none: no
 * hops, and both costs 0 (content it is already producing).
 */
const routes = new WeakMap<Announcement, Route>();

function routeOf(ann: Announcement): Route {
	return routes.get(ann) ?? { hopIDs: [], warmCost: 0, coldCost: 0 };
}

/** The saturation ceiling of a route cost: the largest varint. */
const MAX_ROUTE_COST = 2 ** 62 - 1;

/** Adds two route costs, saturating at {@link MAX_ROUTE_COST}. */
function addCost(a: number, b: number): number {
	const sum = a + b;
	return sum > MAX_ROUTE_COST ? MAX_ROUTE_COST : sum;
}

/**
 * Returns the route suffix `path` is advertised under for a request for
 * `prefix`: the part past the prefix, or "" for a route at or above it, which
 * presents clamped to the prefix (moq-lite-06 §5.1.1). Matching is per path
 * segment: a prefix ends in "/", so a path never matches half a segment.
 * Returns undefined when the route does not meet the prefix.
 */
export function routeSuffix(path: string, prefix: string): suffix | undefined {
	if (path.startsWith(prefix)) {
		return path.substring(prefix.length);
	}
	if (path === "/" || (prefix.startsWith(path) && prefix[path.length] === "/")) {
		return "";
	}
	return undefined;
}

/** A route advertised on the stream by an {@link AnnouncementWriter}. */
interface Advertised {
	/** The Announce ID its ANNOUNCE_START assigned. */
	readonly id: number;
	/** The announcement the route currently advertises. */
	announcement: Announcement;
	/**
	 * Other announcements for the same route, newest last: the route moves to
	 * one with ANNOUNCE_UPDATE when `announcement` ends. Only routes clamped to
	 * the request prefix collect them.
	 */
	standby: Announcement[];
}

/**
 * Writes announcements to a remote peer over an announce stream.
 *
 * Created on the publisher side to respond to an ANNOUNCE_REQUEST from the
 * subscriber. Call {@link init} with a set of initial announcements, then
 * {@link send} for updates.
 *
 * Each route has at most one advertisement on the stream (moq-lite-06 §5.1.1):
 * the first ANNOUNCE_START for it assigns an Announce ID, a change of the
 * announcement behind it travels as ANNOUNCE_UPDATE on that ID, and its end as
 * ANNOUNCE_END.
 */
export class AnnouncementWriter {
	#stream: Stream;
	readonly prefix: TrackPrefix;
	#actives: Map<suffix, Advertised> = new Map();
	#nextId = 0;
	readonly #excludeHop: number;
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	#ready: Promise<void>;
	#resolveInit?: () => void;

	/**
	 * @param excludeHop - The Hop ID the peer declared in its SETUP (0 for
	 * none): a path through it is never advertised to the peer.
	 */
	constructor(
		sessCtx: Context,
		stream: Stream,
		req: AnnounceRequestMessage,
		excludeHop: number = 0,
	) {
		this.#stream = stream;
		this.prefix = validateTrackPrefix(req.prefix);
		this.#excludeHop = excludeHop;

		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);
		this.#ready = new Promise<void>((resolve) => {
			this.#resolveInit = resolve;
		});
	}

	// Whether ann's path runs through the peer's declared Hop ID
	// (moq-lite-06 §5.1.1.1): the peer could only discard it.
	#excluded(ann: Announcement): boolean {
		return this.#excludeHop !== 0 && ann.hopIDs.includes(this.#excludeHop);
	}

	/**
	 * Initialize the writer with a batch of announcements.
	 * Must be called exactly once before {@link send}.
	 * @param anns - Initial announcements to send.
	 */
	async init(anns: Announcement[]): Promise<Error | undefined> {
		for (const announcement of anns) {
			const path = announcement.broadcastPath;
			const sfx = routeSuffix(path, this.prefix);
			if (sfx === undefined) {
				return new Error(
					`Path ${path} does not start with prefix ${this.prefix}`,
				);
			}
			if (!announcement.isActive() || this.#excluded(announcement)) {
				continue;
			}
			const active = this.#actives.get(sfx);
			if (active) {
				if (active.announcement === announcement) continue;
				// Several routes clamp to the request prefix: advertise one,
				// keep the rest to fall back on.
				active.standby.push(announcement);
				continue;
			}
			this.#actives.set(sfx, { id: -1, announcement, standby: [] });
		}

		// ANNOUNCE_OK is sent exactly once, before any announcement, carrying
		// this node's Hop ID (0: not a relay) and the initial count.
		const okMsg = new AnnounceOkMessage({
			hopID: 0,
			activeCount: this.#actives.size,
		});
		const okErr = await okMsg.encode(this.#stream.writable);
		if (okErr) {
			return okErr;
		}

		for (const [sfx, entry] of [...this.#actives.entries()]) {
			const active: Advertised = { ...entry, id: this.#nextId++ };
			this.#actives.set(sfx, active);
			const err = await this.#start(sfx, active.announcement);
			if (err) {
				return err;
			}
			this.#watch(sfx, active);
		}

		this.#resolveInit?.();
		this.#resolveInit = undefined;

		return undefined;
	}

	#start(sfx: suffix, ann: Announcement): Promise<Error | undefined> {
		const r = routeOf(ann);
		return new AnnounceStartMessage({
			suffix: sfx,
			hopIDs: [...r.hopIDs],
			warmCost: r.warmCost,
			coldCost: r.coldCost,
		}).encode(this.#stream.writable);
	}

	#update(active: Advertised): Promise<Error | undefined> {
		const r = routeOf(active.announcement);
		return new AnnounceUpdateMessage({
			announceId: active.id,
			hopIDs: [...r.hopIDs],
			warmCost: r.warmCost,
			coldCost: r.coldCost,
		}).encode(this.#stream.writable);
	}

	// Watches the announcement active advertises, re-advertising or ending the
	// route when it ends.
	#watch(sfx: suffix, active: Advertised): void {
		const ann = active.announcement;
		ann.ended().then(() => this.#ended(sfx, active, ann)).catch(() => {});
	}

	// Handles the end of ann, the announcement behind active: a standby still
	// active takes over with ANNOUNCE_UPDATE; otherwise ANNOUNCE_END.
	async #ended(sfx: suffix, active: Advertised, ann: Announcement): Promise<void> {
		if (this.#actives.get(sfx) !== active || active.announcement !== ann) {
			return;
		}
		if (this.context.err()) {
			return;
		}
		while (active.standby.length > 0) {
			const next = active.standby.pop()!;
			if (!next.isActive() || this.#excluded(next)) continue;
			active.announcement = next;
			this.#watch(sfx, active);
			await this.#update(active);
			return;
		}
		this.#actives.delete(sfx);
		const err = await new AnnounceEndMessage({ announceId: active.id }).encode(
			this.#stream.writable,
		);
		if (err && !(err instanceof WebTransportStreamError)) {
			console.warn(`moq: failed to send ANNOUNCE_END: ${err}`);
		}
	}

	/**
	 * Send a single announcement update after initialization. An active
	 * announcement for a route already advertised by another moves the route
	 * to it with ANNOUNCE_UPDATE, keeping its Announce ID; an inactive one ends
	 * the announcement the route advertises.
	 * @param announcement - The announcement to add or end.
	 */
	async send(announcement: Announcement): Promise<Error | undefined> {
		await this.#ready; // Wait for initialization to complete
		if (this.context.err()) {
			return undefined;
		}

		const path = announcement.broadcastPath;
		const sfx = routeSuffix(path, this.prefix);
		if (sfx === undefined) {
			return new Error(
				`Path ${path} does not start with prefix ${this.prefix}`,
			);
		}

		const active = this.#actives.get(sfx);
		if (!announcement.isActive()) {
			if (!active) {
				return new Error(
					`[AnnouncementWriter] announcement to end for path ${this.prefix}${sfx} is not active`,
				);
			}
			// End the announcement the route advertises; its watch sends the
			// ANNOUNCE_UPDATE or ANNOUNCE_END.
			active.announcement.end();
			return undefined;
		}

		if (this.#excluded(announcement)) {
			return undefined;
		}

		if (active) {
			if (active.announcement === announcement || active.standby.includes(announcement)) {
				return undefined; // Already advertised
			}
			// A second ANNOUNCE_START for an advertised route would be a
			// protocol violation: move it to the newer announcement in place,
			// keeping the older one to fall back on while it lasts.
			const prev = active.announcement;
			if (prev.isActive()) {
				active.standby.push(prev);
			}
			active.announcement = announcement;
			this.#watch(sfx, active);
			return await this.#update(active);
		}

		const adv: Advertised = { id: this.#nextId++, announcement, standby: [] };
		this.#actives.set(sfx, adv);
		const err = await this.#start(sfx, announcement);
		if (err) {
			if (err instanceof WebTransportStreamError) {
				return new AnnounceError(err.code, err.remote);
			}
			return err;
		}
		this.#watch(sfx, adv);

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
		for (const active of this.#actives.values()) {
			active.announcement.end();
		}
		this.#actives.clear();
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
		this.#actives.clear();
		this.#resolveInit?.();
		this.#resolveInit = undefined;
	}
}

/** Session state an {@link AnnouncementReader} applies moq-lite-06's rules with. */
export interface AnnouncementReaderOptions {
	/**
	 * This endpoint's Hop ID; an announcement whose path contains it has
	 * looped back and is discarded. 0 (the default) disables the check.
	 */
	localHopId?: number;
	/**
	 * Resolves the Cost the peer declared in its SETUP, added to both route
	 * costs of every announcement. Called once, at the first announcement.
	 * Omitted means {@link DEFAULT_COST}.
	 */
	peerCost?: () => Promise<number>;
	/**
	 * Closes the session: every Announce Stream violation is fatal to it
	 * (moq-lite-06 §5.1.1). Omitted, a violation only ends the reader.
	 */
	onProtocolViolation?: (reason: string) => void;
}

/** Raised for an Announce Stream message that breaks moq-lite-06 §5.1.1. */
export class AnnounceViolation extends Error {
	constructor(message: string) {
		super(message);
		this.name = "AnnounceViolation";
	}
}

/** The peer's current advertisement of one route on the stream. */
interface ReceivedRoute {
	readonly id: number;
	/**
	 * The announcement handed to the application, or undefined while the
	 * advertisement is discarded (it looped back through this endpoint). The
	 * peer still advertises it, so its id stays live.
	 */
	ann?: Announcement;
}

/**
 * Reads announcements from a remote peer over an announce stream.
 *
 * Created on the subscriber side after sending an ANNOUNCE_REQUEST.
 * Use {@link receive} in a loop to consume incoming announcements.
 *
 * An announced path is a route: a prefix under which the peer claims it can
 * serve, not a promise that a broadcast exists there. By convention a
 * publisher announces each broadcast's exact path. A route the peer holds
 * above the requested prefix arrives clamped to it, as the prefix itself.
 */
export class AnnouncementReader {
	#stream: Stream;
	readonly prefix: string;
	// Live Announce IDs to the route suffix each advertises.
	#ids: Map<number, suffix> = new Map();
	// The peer's current advertisement for each route suffix.
	#advertised: Map<suffix, ReceivedRoute> = new Map();
	#nextId = 0;
	#queue: Queue<Announcement> = new Queue();
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	readonly #sessCtx: Context;
	readonly #options: AnnouncementReaderOptions;
	#closed = false;
	#peerHopID: number = 0;
	#peerCost?: number;

	constructor(
		sessCtx: Context,
		stream: Stream,
		announceRequest: AnnounceRequestMessage,
		options: AnnouncementReaderOptions = {},
	) {
		this.#stream = stream;
		this.#sessCtx = sessCtx;
		this.#options = options;
		const prefix = announceRequest.prefix;
		if (!isValidPrefix(prefix)) {
			throw new Error(`[AnnouncementReader] invalid prefix: ${prefix}.`);
		}
		this.prefix = prefix;
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);

		this.#run().catch((err) => {
			this.#end(err);
		});
	}

	async #run(): Promise<void> {
		// The publisher sends ANNOUNCE_OK exactly once before any
		// announcement. Its Hop ID is the implicit trailing entry of every
		// announcement's Hop ID list on this stream.
		const okMsg = new AnnounceOkMessage({});
		const okErr = await okMsg.decode(this.#stream.readable);
		if (okErr) {
			this.#end(okErr);
			return;
		}
		this.#peerHopID = okMsg.hopID;

		while (!this.context.err()) {
			const [type, n, typeErr] = await readVarint(this.#stream.readable);
			if (typeErr) {
				// A tag cut short after its first byte is malformed; none at all
				// is the stream ending between messages.
				this.#end(
					n > 0 && typeErr instanceof EOFError
						? new AnnounceDecodeError("truncated type")
						: typeErr,
				);
				return;
			}
			const err = await this.#handle(type);
			if (err) {
				this.#end(err);
				return;
			}
		}
	}

	async #handle(type: number): Promise<Error | undefined> {
		switch (type) {
			case AnnounceMessageTypes.Start: {
				const msg = new AnnounceStartMessage();
				const err = await msg.decode(this.#stream.readable);
				if (err) return err;
				const route = await this.#received(msg.hopIDs, msg.warmCost, msg.coldCost);
				if (route instanceof Error) return route;
				return this.#start(msg.suffix, route);
			}
			case AnnounceMessageTypes.End: {
				const msg = new AnnounceEndMessage();
				const err = await msg.decode(this.#stream.readable);
				if (err) return err;
				return this.#retract(msg.announceId);
			}
			case AnnounceMessageTypes.Update: {
				const msg = new AnnounceUpdateMessage();
				const err = await msg.decode(this.#stream.readable);
				if (err) return err;
				const route = await this.#received(msg.hopIDs, msg.warmCost, msg.coldCost);
				if (route instanceof Error) return route;
				return this.#update(msg.announceId, route);
			}
			default:
				return new AnnounceViolation(`moq: unknown announcement type ${type}`);
		}
	}

	// Reconstructs an advertisement's routing metadata: its Hop ID list plus
	// the peer's implicit ANNOUNCE_OK Hop ID, and both costs plus the cost the
	// peer declared for its link, saturating.
	async #received(
		hopIDs: number[],
		warm: number,
		cold: number,
	): Promise<Route | Error> {
		const dup = duplicateHopID(hopIDs);
		if (dup !== undefined) {
			return new AnnounceViolation(`moq: repeated hop id ${dup} in one announcement`);
		}
		if (this.#peerCost === undefined) {
			this.#peerCost = this.#options.peerCost ? await this.#options.peerCost() : DEFAULT_COST;
		}
		return {
			hopIDs: [...hopIDs, this.#peerHopID],
			warmCost: addCost(warm, this.#peerCost),
			coldCost: addCost(cold, this.#peerCost),
		};
	}

	// Whether a route runs back through this endpoint (moq-lite-06 §5.1.1.1).
	#loops(route: Route): boolean {
		const own = this.#options.localHopId ?? 0;
		return own !== 0 && route.hopIDs.includes(own);
	}

	#start(sfx: suffix, route: Route): Error | undefined {
		const id = this.#nextId++;
		if (this.#advertised.has(sfx)) {
			return new AnnounceViolation(
				`moq: ANNOUNCE_START for an already advertised route ${JSON.stringify(sfx)}`,
			);
		}
		const entry: ReceivedRoute = { id };
		this.#advertised.set(sfx, entry);
		this.#ids.set(id, sfx);
		if (!this.#loops(route)) {
			entry.ann = this.#activate(sfx, route);
		}
		return undefined;
	}

	#retract(id: number): Error | undefined {
		const sfx = this.#ids.get(id);
		if (sfx === undefined) {
			return new AnnounceViolation(
				`moq: ANNOUNCE_END for unknown or retired Announce ID ${id}`,
			);
		}
		this.#ids.delete(id);
		const entry = this.#advertised.get(sfx);
		this.#advertised.delete(sfx);
		entry?.ann?.end();
		return undefined;
	}

	#update(id: number, route: Route): Error | undefined {
		const sfx = this.#ids.get(id);
		if (sfx === undefined) {
			return new AnnounceViolation(
				`moq: ANNOUNCE_UPDATE for unknown or retired Announce ID ${id}`,
			);
		}
		const entry = this.#advertised.get(sfx)!; // reason: #ids and #advertised change together
		const admitted = !this.#loops(route);
		if (admitted && entry.ann) {
			// In place: the announcement, and the subscriptions under it,
			// are undisturbed.
			routes.set(entry.ann, route);
		} else if (admitted) {
			entry.ann = this.#activate(sfx, route);
		} else if (entry.ann) {
			entry.ann.end();
			entry.ann = undefined;
		}
		return undefined;
	}

	#activate(sfx: suffix, route: Route): Announcement {
		const announcement = new Announcement(
			validateBroadcastPath(this.prefix + sfx),
			this.context.done(),
		);
		routes.set(announcement, route);
		this.#queue.enqueue(announcement);
		return announcement;
	}

	// Stops reading for err. A rule broken on the stream closes the session
	// (moq-lite-06 §5.1.1); a stream the peer ended or reset only ends the
	// reader. Either way every announcement on it ends with the reader's
	// context.
	#end(err: unknown): void {
		if (err instanceof AnnounceViolation || err instanceof AnnounceDecodeError) {
			this.#options.onProtocolViolation?.(err.message);
		} else if (err instanceof WebTransportStreamError) {
			err = new AnnounceError(err.code, err.remote);
		}
		this.#fail(err);
	}

	// fail cancels the reader context with cause and closes the queue, the two
	// things receive() needs to unblock with an error instead of hanging.
	#fail(err: unknown): void {
		this.#cancelFunc(
			err instanceof Error ? err : new Error("moq: announce stream failed", { cause: err }),
		);
		this.#queue.close();
	}

	/**
	 * Wait for the next active announcement. An ANNOUNCE_UPDATE changes an
	 * announcement in place (see {@link Announcement.hopIDs}) and returns
	 * nothing new.
	 * @param signal - A promise that, when resolved, cancels the wait.
	 * @returns The next {@link Announcement}, or an Error.
	 */
	async receive(
		signal: Promise<void>,
	): Promise<[Announcement, undefined] | [undefined, Error]> {
		const ctx = watchPromise(this.context, signal);
		let stopped = false;
		const stop = signal.then(() => {
			stopped = true;
		}, () => {
			stopped = true;
		});

		while (true) {
			const announcement = await this.#queue.dequeue(stop);
			if (announcement === undefined) {
				if (stopped) {
					return [undefined, ctx.err() ?? new Error("moq: receive canceled")];
				}
				// Queue closed (reader failed/closed). Surface the cause, if any.
				return [undefined, ctx.err() ?? new Error("Queue is closed and empty")];
			}

			if (announcement.isActive()) {
				return [announcement, undefined];
			}

			const err = ctx.err();
			if (err) {
				return [undefined, err];
			}

			// The announcement ended while queued: skip it.
		}
	}

	async close(): Promise<void> {
		// A reader that failed because the publisher ended the stream still
		// closes its own side; one whose session has ended has nothing to close.
		if (this.#closed || this.#sessCtx.err()) {
			return;
		}
		this.#closed = true;

		this.#cancelFunc(undefined);

		await this.#stream.writable.close();
		this.#advertised.clear();
		this.#ids.clear();
		this.#queue.close();
	}

	async closeWithError(code: AnnounceErrorCode): Promise<void> {
		if (this.#closed || this.#sessCtx.err()) {
			return;
		}
		this.#closed = true;
		const cause = new WebTransportStreamError(
			{ source: "stream", streamErrorCode: code },
			false,
		);
		this.#cancelFunc(cause);
		await this.#stream.writable.cancel(code);
		await this.#stream.readable.cancel(code);
		this.#advertised.clear();
		this.#ids.clear();
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
	#done: Promise<void>;
	#signalFunc: () => void;
	#active: boolean = true;

	constructor(path: string, signal: Promise<void>, hopIDs: number[] = []) {
		this.broadcastPath = validateBroadcastPath(path);
		if (hopIDs.length > 0) {
			routes.set(this, { hopIDs, warmCost: 0, coldCost: 0 });
		}

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

	/**
	 * The hop path the announcement traversed, from the origin to the peer it
	 * arrived from; a 0 entry is a hop that withheld its identity. An
	 * ANNOUNCE_UPDATE may replace it while the announcement stays active.
	 * Empty for a local announcement.
	 */
	get hopIDs(): readonly number[] {
		return routeOf(this).hopIDs;
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
