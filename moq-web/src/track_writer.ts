import { GroupWriter } from "./group_stream.ts";
import type { Info } from "./info.ts";
import type { Context } from "@okdaichi/golikejs/context";
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
	#openUniStreamFunc: () => Promise<
		[SendStream, undefined] | [undefined, Error]
	>;
	#groups: GroupWriter[] = [];
	#groupCount: number = 0;

	constructor(
		broadcastPath: BroadcastPath,
		trackName: string,
		subscribeStream: ReceiveSubscribeStream,
		openUniStreamFunc: () => Promise<
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
	 * @returns A {@link GroupWriter} for writing frames.
	 */
	async openGroup(): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		this.#groupCount++;
		return this.#openGroupWithSequence(this.#groupCount);
	}

	/**
	 * Open a new group at a specific sequence number.
	 * @param seq - The group sequence number.
	 */
	async openGroupAt(seq: number): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		if (seq > this.#groupCount) {
			this.#groupCount = seq;
		}
		return this.#openGroupWithSequence(seq);
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
	): Promise<[GroupWriter, undefined] | [undefined, Error]> {
		let err: Error | undefined;
		err = await this.#subscribeStream.ensureInfo();
		if (err) {
			return [undefined, err];
		}

		let writer: SendStream | undefined;
		[writer, err] = await this.#openUniStreamFunc();
		if (err) {
			return [undefined, err];
		}

		[, err] = await writeVarint(writer!, UniStreamTypes.GroupStreamType);
		if (err) {
			return [undefined, new Error(`Failed to write stream type: ${err}`)];
		}

		const msg = new GroupMessage({
			subscribeId: this.subscribeId,
			sequence: seq,
		});
		err = await msg.encode(writer!);
		if (err) {
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
