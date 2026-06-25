import { GroupWriter } from "./group_stream.ts";
import type { Info } from "./info.ts";
import { type Context, ContextCancelledError, toAbortSignal } from "@okdaichi/golikejs/context";
import type { ReceiveSubscribeStream, SubscribeDrop, TrackConfig } from "./subscribe_stream.ts";
import type { SendStream } from "./internal/webtransport/mod.ts";
import { UniStreamTypes } from "./stream_type.ts";
import { GroupMessage, writeVarint } from "./internal/message/mod.ts";
import type { BroadcastPath } from "./broadcast_path.ts";
import type { SubscribeErrorCode } from "./error.ts";
import { GroupErrorCode } from "./error.ts";

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
	#openUniStreamFunc: (signal?: AbortSignal) => Promise<
		[SendStream, undefined] | [undefined, Error]
	>;
	#groups: GroupWriter[] = [];
	#groupCount: number = 0;

	constructor(
		broadcastPath: BroadcastPath,
		trackName: string,
		subscribeStream: ReceiveSubscribeStream,
		openUniStreamFunc: (signal?: AbortSignal) => Promise<
			[SendStream, undefined] | [undefined, Error]
		>,
	) {
		this.broadcastPath = broadcastPath;
		this.trackName = trackName;
		this.#subscribeStream = subscribeStream;
		this.#openUniStreamFunc = openUniStreamFunc;
	}

	get context(): Context {
		return this.#subscribeStream.context;
	}

	get subscribeId(): number {
		return this.#subscribeStream.subscribeId;
	}

	get config(): TrackConfig {
		return this.#subscribeStream.trackConfig;
	}

	/**
	 * Open a new group with an auto-incremented sequence number.
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

	async updated(): Promise<void> {
		return this.#subscribeStream.updated();
	}

	async #openGroupWithSequence(
		seq: number,
		signal?: AbortSignal,
	): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		// Mirror Go openGroupWithSequence (track_writer.go:298): if the track
		// context is already done, fail fast without opening a stream.
		if (this.context.err()) {
			return [undefined, this.context.err()!];
		}

		// ensureInfo is not raced against the signal (Go passes no ctx here).
		let err: Error | undefined;
		err = await this.#subscribeStream.ensureInfo();
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
			const reason = (effective as unknown as { reason?: unknown }).reason;
			return [
				undefined,
				reason instanceof Error ? reason : new ContextCancelledError(),
			];
		}

		// Thread the effective signal down to the stream-open, mirroring Go's
		// openUniStreamFunc(ctx) (track_writer.go:314).
		let writer: SendStream | undefined;
		[writer, err] = await this.#openUniStreamFunc(effective);
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
		});
		err = await msg.encode(writer!);
		if (err) {
			await writer!.cancel(GroupErrorCode.InternalError).catch(() => {});
			return [undefined, new Error("Failed to create group message")];
		}

		const group = new GroupWriter(this.context, writer!, msg);

		this.#groups.push(group);

		return [group, undefined];
	}

	/**
	 * Write publisher {@link Info} to the subscriber.
	 * @param info - Track information to send.
	 */
	async writeInfo(info: Info): Promise<Error | undefined> {
		const err = await this.#subscribeStream.writeInfo(info);
		if (err) {
			return err;
		}

		return undefined;
	}

	async closeWithError(code: SubscribeErrorCode): Promise<void> {
		// Cancel all groups with the error first
		await Promise.allSettled(this.#groups.map(
			(group) => group.cancel(GroupErrorCode.PublishAborted),
		));

		// Then close the subscribe stream with the error
		await this.#subscribeStream.closeWithError(code);
	}

	async close(): Promise<void> {
		await Promise.allSettled(this.#groups.map(
			(group) => group.close(),
		));

		await this.#subscribeStream.close();
	}
}
