import { GroupWriter } from "./group_stream.ts";
import { type Context, ContextCancelledError, toAbortSignal } from "@okdaichi/golikejs/context";
import type { ReceiveSubscribeStream, SubscribeDrop, TrackConfig } from "./subscribe_stream.ts";
import type { SendStream } from "./internal/webtransport/mod.ts";
import { UniStreamTypes } from "./stream_type.ts";
import { GroupMessage, writeVarint } from "./internal/message/mod.ts";
import { type BroadcastPath, isValidBroadcastPath } from "./broadcast_path.ts";
import type { SubscribeErrorCode } from "./error.ts";
import { GroupErrorCode, InvalidBroadcastPathError, RedirectNotSupportedError } from "./error.ts";
import { SubscribeRedirectMessage } from "./internal/message/mod.ts";
import { GroupAges } from "./group_ages.ts";
import { DEFAULT_TIMESCALE } from "./info.ts";

/**
 * Returned by {@link TrackWriter.openGroup} and {@link TrackWriter.openGroupAt}
 * for a group outside the subscription's range: below its `startGroup` floor
 * or above its `endGroup`.
 */
export class GroupOutOfRangeError extends Error {
	constructor(seq: number) {
		super(`moq: group ${seq} is outside the subscription's range`);
		this.name = "GroupOutOfRangeError";
	}
}

/** Where {@link TrackWriter.redirect} sends a subscriber. */
export interface RedirectTarget {
	/** The broadcast to subscribe to instead. */
	readonly broadcastPath: BroadcastPath;
	/** The track to subscribe to instead; omitted or empty keeps the subscriber's. */
	readonly trackName?: string;
}

/**
 * Reports whether the subscriber advertised that it follows redirects, waiting
 * for its SETUP until signal aborts; an error when the session or signal ends
 * the wait first.
 */
type PeerFollowsRedirect = (
	signal: AbortSignal,
) => Promise<
	readonly [follows: boolean, error: undefined] | readonly [follows: undefined, error: Error]
>;

/**
 * Publisher-side handle for writing groups to a subscribed track.
 *
 * Created by the {@link TrackMux} when an incoming subscription is matched.
 * Use {@link openGroup} to start writing frames.
 */
export class TrackWriter {
	/** The broadcast path this track belongs to. */
	broadcastPath: BroadcastPath;
	/** The track name within the broadcast. */
	trackName: string;
	#subscribeStream: ReceiveSubscribeStream;
	#openUniStreamFunc: (options?: { signal?: AbortSignal }) => Promise<
		[SendStream, undefined] | [undefined, Error]
	>;
	#groups: GroupWriter[] = [];
	#groupCount: number = 0;
	// Set once group 0 is opened, which openGroupAt alone does without
	// advancing #groupCount.
	#usedZero: boolean = false;
	#peerFollowsRedirect?: PeerFollowsRedirect;
	readonly #ages: GroupAges;

	constructor(
		broadcastPath: BroadcastPath,
		trackName: string,
		subscribeStream: ReceiveSubscribeStream,
		openUniStreamFunc: (options?: { signal?: AbortSignal }) => Promise<
			[SendStream, undefined] | [undefined, Error]
		>,
		peerFollowsRedirect?: PeerFollowsRedirect,
		timescale: number = DEFAULT_TIMESCALE,
	) {
		this.broadcastPath = broadcastPath;
		this.trackName = trackName;
		this.#subscribeStream = subscribeStream;
		this.#openUniStreamFunc = openUniStreamFunc;
		this.#peerFollowsRedirect = peerFollowsRedirect;
		// Max Age is measured in the track's own timestamps.
		this.#ages = new GroupAges(() => this.config.maxAge, timescale);
	}

	/**
	 * Answers the subscription with SUBSCRIBE_REDIRECT instead of serving it,
	 * sending the subscriber to another track on the same session, as Go's
	 * `TrackWriter.Redirect` does. An omitted `trackName` keeps the
	 * subscriber's, so a whole broadcast can be redirected with one rule. It
	 * must come before the first group, and ends the subscription: after it, no
	 * group opens and close sends nothing.
	 *
	 * It waits for the subscriber's SETUP, up to 5 s, until the subscription
	 * ends, or until `options.signal` aborts. It returns a
	 * {@link RedirectNotSupportedError}, leaving the subscription as it was,
	 * when the subscriber did not advertise that it follows redirects (serve or
	 * refuse it instead); a {@link SubscriptionAnsweredError} once the
	 * subscription was answered; an {@link InvalidBroadcastPathError} for a
	 * path that is not one; and the session's or the signal's error when either
	 * ends the wait.
	 */
	async redirect(
		to: RedirectTarget,
		options?: { signal?: AbortSignal },
	): Promise<Error | undefined> {
		if (!isValidBroadcastPath(to.broadcastPath)) {
			return new InvalidBroadcastPathError(to.broadcastPath);
		}
		if (!this.#peerFollowsRedirect) {
			return new RedirectNotSupportedError();
		}
		const trackSignal = toAbortSignal(this.context);
		const signal = options?.signal
			? AbortSignal.any([trackSignal, options.signal])
			: trackSignal;
		const [follows, err] = await this.#peerFollowsRedirect(signal);
		if (err !== undefined) {
			return err;
		}
		if (!follows) {
			return new RedirectNotSupportedError();
		}
		// An empty name, as an omitted one, keeps the subscriber's (as Go does).
		const trackName = to.trackName === undefined || to.trackName === ""
			? this.trackName
			: to.trackName;
		return await this.#subscribeStream.writeRedirect(
			new SubscribeRedirectMessage({ broadcastPath: to.broadcastPath, trackName }),
		);
	}

	get context(): Context {
		return this.#subscribeStream.context;
	}

	get subscribeId(): number {
		return this.#subscribeStream.subscribeId;
	}

	/**
	 * The subscriber's current config. It reflects the latest SUBSCRIBE_UPDATE
	 * only if the publisher is draining updates via readUpdate: it advances when
	 * readUpdate applies an update, not on its own. A publisher that never calls
	 * readUpdate sees the initial (SUBSCRIBE-time) config.
	 */
	get config(): TrackConfig {
		return this.#subscribeStream.trackConfig;
	}

	/**
	 * Open a new group with an auto-incremented sequence number.
	 *
	 * A sequence below the subscription's `startGroup`, or above its
	 * `endGroup`, is refused with a {@link GroupOutOfRangeError}. When the peer
	 * bounded its subscription by frame (moq-lite-06 §3.6), the start and end
	 * groups are sent partially: write them whole and the {@link GroupWriter}
	 * sends only the frames asked for.
	 *
	 * The group opens on a fresh unidirectional stream; when the peer's
	 * concurrent-stream limit is reached this blocks until a stream is granted.
	 * Pass `options.signal` to cancel or time out the open. If aborted, no group
	 * stream remains open.
	 * @param options - Optional cancellation `signal`.
	 * @returns A {@link GroupWriter} for writing frames, or an error.
	 */
	async openGroup(
		options?: { signal?: AbortSignal },
	): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		this.#groupCount++;
		return this.#openGroupWithSequence(this.#groupCount, options?.signal);
	}

	/**
	 * Open a new group at a specific sequence number.
	 * @param seq - The group sequence number.
	 * @param options - Optional cancellation `signal`.
	 */
	async openGroupAt(
		seq: number,
		options?: { signal?: AbortSignal },
	): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		if (seq > this.#groupCount) {
			this.#groupCount = seq;
		}
		if (seq === 0) {
			this.#usedZero = true;
		}
		return this.#openGroupWithSequence(seq, options?.signal);
	}

	/** Advance the group counter by `n` without opening groups. */
	skipGroups(n: number): void {
		this.#groupCount += n;
	}

	/**
	 * Notify the subscriber that groups in the given range were dropped.
	 * @param drop - Range and error code of dropped groups.
	 */
	async dropGroups(drop: SubscribeDrop): Promise<Error | undefined> {
		if (drop.startGroup === 0 || drop.endGroup === 0) {
			return new Error(`invalid drop range: start=${drop.startGroup} end=${drop.endGroup}`);
		}
		if (drop.startGroup > drop.endGroup) {
			return new Error(`invalid drop range: start=${drop.startGroup} end=${drop.endGroup}`);
		}
		return this.#subscribeStream.writeDrop(drop);
	}

	async dropNextGroups(n: number, code: SubscribeErrorCode): Promise<Error | undefined> {
		if (n === 0) {
			return undefined;
		}
		const start = this.#groupCount + 1;
		const end = this.#groupCount + n;
		this.#groupCount = end;

		return this.dropGroups({
			startGroup: start,
			endGroup: end,
			errorCode: code,
		});
	}

	/**
	 * readUpdate awaits the next SUBSCRIBE_UPDATE from the subscriber, applies it
	 * as the track's current config (observable via `config`), and returns it.
	 * The error is set once the subscribe stream ends or is closed — the terminal
	 * signal for a read loop.
	 *
	 * A publisher that wants to follow subscriber updates calls it in a loop; it
	 * is the only thing that keeps `config` fresh. Publishers that ignore updates
	 * simply never call it, spending nothing on update reading. Concurrent callers
	 * are serialized but each receives a distinct update in unspecified order —
	 * prefer a single caller.
	 */
	async readUpdate(): Promise<[TrackConfig, undefined] | [undefined, Error]> {
		return this.#subscribeStream.readUpdate();
	}

	async #openGroupWithSequence(
		seq: number,
		signal?: AbortSignal,
	): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		// Mirror Go openGroupWithSequence (track_writer.go:298): if the track
		// context is already done, fail fast without opening a stream. This
		// synchronous check is load-bearing — toAbortSignal(this.context) only
		// reflects cancellation on a later microtask, so the effective.aborted
		// check below would not yet see it. Do not remove.
		if (this.context.err()) {
			return [undefined, this.context.err()!];
		}

		// Group Start is a floor the publisher MUST NOT deliver below
		// (moq-lite-06 §7.9), and Group End the last group to deliver.
		const config = this.config;
		if (seq < config.startGroup || (config.endGroup !== 0 && seq > config.endGroup)) {
			return [undefined, new GroupOutOfRangeError(seq)];
		}
		// A partial group goes only to a subscriber that asked for one
		// (moq-lite-06 §3.6): the start group from the peer's Frame Start, the
		// end group up to its Frame End, every other group whole.
		const range = this.#subscribeStream.frameRange;
		const frameStart = seq === config.startGroup ? range.frameStart : 0;
		const frameEnd = config.endGroup !== 0 && seq === config.endGroup ? range.frameEnd : 0;

		// ensureOk is not raced against the signal (Go passes no ctx here).
		// The first opened group resolves the subscription's absolute start.
		let err: Error | undefined;
		err = await this.#subscribeStream.ensureOk(seq);
		if (err) {
			return [undefined, err];
		}

		// Effective cancellation: the track context OR the caller's signal.
		// AbortSignal.any composes them; with no caller signal the track context
		// alone bounds the open (Go's `ctx.Done()==nil → w.Context()`).
		const trackSignal = toAbortSignal(this.context);
		const effective: AbortSignal = signal
			? AbortSignal.any([trackSignal, signal])
			: trackSignal;

		// §1 Abort before starting: do not open a stream.
		if (effective.aborted) {
			const reason = effective.reason;
			return [
				undefined,
				reason instanceof Error ? reason : new ContextCancelledError(),
			];
		}

		// Thread the effective signal down to the stream-open, mirroring Go's
		// openUniStreamFunc(ctx) (track_writer.go:314).
		let writer: SendStream | undefined;
		[writer, err] = await this.#openUniStreamFunc({ signal: effective });
		if (err) {
			return [undefined, err];
		}

		// §3 A stream is now allocated. Any failure before the group header is
		// fully written must cancel the stream so it is never left half-open.
		// (Mirrors Go stream.CancelWrite on encode error; previously leaked.)
		[, err] = await writeVarint(writer!, UniStreamTypes.GroupStreamType);
		if (err) {
			await writer!.cancel(GroupErrorCode.InternalError).catch(() => {});
			return [undefined, new Error(`Failed to write stream type: ${err}`)];
		}

		const msg = new GroupMessage({
			subscribeId: this.subscribeId,
			sequence: seq,
			frameStart,
		});
		err = await msg.encode(writer!);
		if (err) {
			await writer!.cancel(GroupErrorCode.InternalError).catch(() => {});
			return [undefined, new Error("Failed to create group message")];
		}

		const group = new GroupWriter(this.context, writer!, msg, { frameEnd, ages: this.#ages });

		this.#groups.push(group);

		return [group, undefined];
	}

	async closeWithError(code: SubscribeErrorCode): Promise<void> {
		// Cancel all groups with the error first
		await Promise.allSettled(this.#groups.map(
			(group) => group.cancel(GroupErrorCode.InternalError),
		));

		// Then close the subscribe stream with the error
		await this.#subscribeStream.closeWithError(code);
	}

	async close(): Promise<void> {
		await Promise.allSettled(this.#groups.map(
			(group) => group.close(),
		));

		// Signal that no group at or after the one following the last sequence
		// used will be produced: SUBSCRIBE_END's Group is exclusive. When no
		// group was used it is 0, and without a preceding SUBSCRIBE_OK it says
		// the track ended with no matching groups.
		const end = this.#groupCount === 0 && !this.#usedZero ? 0 : this.#groupCount + 1;
		await this.#subscribeStream.writeEnd(end);

		await this.#subscribeStream.close();
	}
}
