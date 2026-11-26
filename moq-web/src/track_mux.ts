import type { AnnouncementWriter } from "./announce_stream.ts";
import { Announcement, routeSuffix } from "./announce_stream.ts";
import type { BroadcastPath } from "./broadcast_path.ts";
import { NotFoundTrackHandler } from "./broadcast.ts";
import { SubscribeErrorCode } from "./error.ts";

import type { RedirectTarget, TrackWriter } from "./track_writer.ts";
import type { TrackPrefix } from "./track_prefix.ts";
import { DEFAULT_TIMESCALE, type Info } from "./info.ts";

type AnnouncedTrackHandler = {
	announcement: Announcement;
	handler: TrackHandler;
};

/**
 * Multiplexes incoming subscribe and announce requests to registered {@link TrackHandler}s.
 *
 * Tracks are registered via {@link announce} (or the convenience helpers {@link publish}
 * and {@link publishFunc}), then automatically dispatched when a subscriber requests them.
 */
export class TrackMux {
	#handlers: Map<BroadcastPath, AnnouncedTrackHandler> = new Map();
	// Counts the paths in #handlers by length; #routeLensDesc lists those
	// lengths, longest first. #route tries only prefixes of those lengths, so
	// its cost does not grow with the path, which is peer input and may hold
	// thousands of segments.
	#routeLens: Map<number, number> = new Map();
	#routeLensDesc: number[] = [];
	#announcers: Map<TrackPrefix, Set<AnnouncementWriter>> = new Map();

	constructor() {}

	/**
	 * Register a track announcement with its handler.
	 * @param announcement - The {@link Announcement} describing the broadcast path.
	 * @param handler - The {@link TrackHandler} invoked when a subscriber requests this track.
	 */
	async announce(
		announcement: Announcement,
		handler: TrackHandler,
	): Promise<void> {
		const path = announcement.broadcastPath;

		if (!announcement.isActive()) {
			console.warn(`[TrackMux] announcement is not active for path: ${path}`);
			return;
		}

		const existing = this.#handlers.get(path);
		if (existing !== undefined && existing.announcement !== announcement) {
			console.warn(`[TrackMux] track already announced for path: ${path}`);

			// End the old announcement
			existing.announcement.end();
		}

		// Register the new announcement and handler
		if (existing === undefined) {
			this.#addRouteLen(path.length);
		}
		this.#handlers.set(path, { announcement, handler });

		const completePromises: Promise<void>[] = [];

		for (const [prefix, announcers] of this.#announcers.entries()) {
			// A route covers every path beneath it, so a request for a prefix
			// at or below it sees it too, clamped (moq-lite-06 §5.1.1).
			if (routeSuffix(path, prefix) !== undefined) {
				const sendPromises: Promise<void>[] = [];
				const failed: AnnouncementWriter[] = [];

				// Notify all announcers for this prefix
				for (const announcer of announcers) {
					const sendPromise = announcer.send(announcement).then((err) => {
						if (err) {
							console.warn(
								`[TrackMux] failed to announce track for path: ${path} to prefix: ${prefix}: ${err}`,
							);
							failed.push(announcer);
						}
					});
					sendPromises.push(sendPromise);
				}

				// Wait for all sends to complete and then clean up failed announcers
				const completedPromise = Promise.all(sendPromises).then(() => {
					for (const failedAnnouncer of failed) {
						announcers.delete(failedAnnouncer);
					}
					if (announcers.size === 0) {
						this.#announcers.delete(prefix);
					}
				});

				completePromises.push(completedPromise);
			}
		}

		// Wait for all notifications to complete
		await Promise.all(completePromises);

		// Wait for the announcement to end
		announcement.ended().then(() => {
			// Only remove the handler if the stored announcement is the same
			// instance that ended. This prevents a replaced announcement from
			// causing the newly-registered handler to be removed.
			const current = this.#handlers.get(path);
			if (current && current.announcement === announcement) {
				this.#handlers.delete(path);
				this.#removeRouteLen(path.length);
			}
		}).catch(() => {});
	}

	/**
	 * Convenience wrapper: create an {@link Announcement} and register it with a handler.
	 * @param ctx - Promise whose resolution ends the announcement.
	 * @param path - Broadcast path.
	 * @param handler - Track handler.
	 */
	async publish(
		ctx: Promise<void>,
		path: BroadcastPath,
		handler: TrackHandler,
	) {
		await this.announce(new Announcement(path, ctx), handler);
	}

	/**
	 * Like {@link publish}, but accepts a plain function instead of a {@link TrackHandler} object.
	 */
	async publishFunc(
		ctx: Promise<void>,
		path: BroadcastPath,
		handler: (trackWriter: TrackWriter) => void | Promise<void>,
	) {
		await this.publish(ctx, path, { serveTrack: handler });
	}

	#addRouteLen(n: number): void {
		const count = (this.#routeLens.get(n) ?? 0) + 1;
		this.#routeLens.set(n, count);
		if (count === 1) {
			const i = this.#routeLensDesc.findIndex((e) => e < n);
			this.#routeLensDesc.splice(i === -1 ? this.#routeLensDesc.length : i, 0, n);
		}
	}

	#removeRouteLen(n: number): void {
		const count = (this.#routeLens.get(n) ?? 0) - 1;
		if (count > 0) {
			this.#routeLens.set(n, count);
			return;
		}
		this.#routeLens.delete(n);
		this.#routeLensDesc = this.#routeLensDesc.filter((e) => e !== n);
	}

	// Returns the handler of the most specific announced route covering
	// path: path itself, then the longest published route that is a leading
	// run of its segments ("/a/b", "/a/", "/a", "/"). Only lengths a published
	// path has are tried. A candidate is a leading part of path, so it starts
	// with "/"; it is spelled as a template so it types as a BroadcastPath.
	#route(path: BroadcastPath): AnnouncedTrackHandler | undefined {
		const exact = this.#handlers.get(path);
		if (exact) return exact;
		for (const n of this.#routeLensDesc) {
			// path[:n] covers path when it ends a segment there: it ends in
			// "/" itself, or path continues with "/".
			if (n >= path.length || (path[n - 1] !== "/" && path[n] !== "/")) continue;
			const route = this.#handlers.get(`/${path.substring(1, n)}`);
			if (route) return route;
		}
		return undefined;
	}

	/**
	 * Look up the immutable publisher properties for a track, used to answer
	 * TRACK_INFO requests. The path resolves as in {@link serveTrack}. Returns
	 * `undefined` when no announced route covers the path or the handler
	 * reports the track as unknown. Handlers that do
	 * not implement {@link TrackInfoProvider} are served with defaults.
	 */
	trackInfo(path: BroadcastPath, name: string): Info | undefined {
		const announced = this.#route(path);
		if (!announced) {
			return undefined;
		}
		const provider = announced.handler as Partial<TrackInfoProvider>;
		if (typeof provider.trackInfo === "function") {
			return provider.trackInfo(name);
		}
		// Handler exists but declares no explicit properties: serve defaults.
		return { priority: 0, maxAge: 0, timescale: DEFAULT_TIMESCALE };
	}

	/**
	 * Dispatch an incoming subscription to the handler of the most specific
	 * announced route covering its path (moq-lite-06 §5.1.1): the path itself,
	 * or else the longest announced route above it, so a handler published at
	 * `/live` also serves `/live/alice/cam1` and reads the full path from
	 * `track.broadcastPath`. If no route covers the path, responds with
	 * {@link NotFoundTrackHandler}.
	 */
	async serveTrack(track: TrackWriter): Promise<void> {
		const path = track.broadcastPath;
		const announced = this.#route(path);
		if (!announced) {
			console.warn(`[TrackMux] no handler for track for path: ${path}`);
			await NotFoundTrackHandler.serveTrack(track);
			return;
		}

		const stop = announced.announcement.afterFunc(() => {
			track.close();
		});

		await announced.handler.serveTrack(track);

		// Ensure cleanup after serving
		stop();
	}

	/**
	 * Begin serving announcements to a remote announce writer.
	 * Sends existing matching announcements as the initial batch, then
	 * blocks until the writer's context ends.
	 */
	async serveAnnouncement(
		writer: AnnouncementWriter,
		prefix: TrackPrefix,
	): Promise<void> {
		let announced: AnnouncedTrackHandler;
		const init: Announcement[] = [];
		for (announced of this.#handlers.values()) {
			// Routes at or above the prefix are included, clamped to it.
			if (routeSuffix(announced.announcement.broadcastPath, prefix) !== undefined) {
				init.push(announced.announcement);
			}
		}

		// Initialize the announcers map for this prefix if it doesn't exist
		if (!this.#announcers.has(prefix)) {
			this.#announcers.set(prefix, new Set());
		}

		// Register the writer as an announcer for this prefix
		const announcers = this.#announcers.get(prefix)!;
		announcers.add(writer);

		// Send initial announcements
		await writer.init(init);

		// Wait for the context to be done
		await writer.context.done();

		// Remove the announcer when the context is done
		announcers.delete(writer);
		if (announcers.size === 0) {
			this.#announcers.delete(prefix);
		}
	}

	/** Close all announcers and clear all registered handlers. */
	async close(): Promise<void> {
		const closePromises: Promise<void>[] = [];
		for (const announcers of this.#announcers.values()) {
			for (const announcer of announcers) {
				closePromises.push(announcer.close());
			}
		}
		await Promise.allSettled(closePromises);
		this.#announcers.clear();
		this.#handlers.clear();
		this.#routeLens.clear();
		this.#routeLensDesc = [];
	}
}

/** Shared default {@link TrackMux} instance used when none is provided. */
export const DefaultTrackMux: TrackMux = new TrackMux();

/**
 * Interface for handling incoming track subscriptions.
 *
 * Implement `serveTrack` to write groups into the provided {@link TrackWriter}.
 */
export interface TrackHandler {
	/**
	 * Called when a subscriber requests a track.
	 * @param trackWriter - Writer for sending groups to the subscriber.
	 */
	serveTrack(trackWriter: TrackWriter): void | Promise<void>;
}

/**
 * Optionally implemented by {@link TrackHandler}s that can answer TRACK_INFO
 * requests with a track's immutable publisher properties.
 */
export interface TrackInfoProvider {
	/**
	 * Returns the publisher properties for the named track, or `undefined`
	 * when the track does not exist.
	 */
	trackInfo(name: string): Info | undefined;
}

/**
 * Returns a {@link TrackHandler} that answers every subscription it receives
 * with a redirect to `to`, as Go's `moqt.RedirectHandler` and
 * `http.RedirectHandler` do. An omitted or empty `trackName` keeps the
 * subscriber's track name. When {@link TrackWriter.redirect} returns an error
 * (for example, {@link RedirectNotSupportedError}), the subscription is closed
 * with {@link SubscribeErrorCode.TrackNotFound}.
 */
export function redirectHandler(to: RedirectTarget): TrackHandler {
	const target: RedirectTarget = {
		broadcastPath: to.broadcastPath,
		...(to.trackName !== undefined ? { trackName: to.trackName } : {}),
	};
	return {
		async serveTrack(trackWriter: TrackWriter): Promise<void> {
			const err = await trackWriter.redirect(target);
			if (err !== undefined) {
				await trackWriter.closeWithError(SubscribeErrorCode.TrackNotFound);
			}
		},
	};
}
