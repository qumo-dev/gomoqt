import type { SubscribeMessage } from "./internal/message/mod.ts";
import {
	readVarint,
	SubscribeDropMessage,
	SubscribeEndMessage,
	SubscribeOkMessage,
	SubscribeUpdateMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import type { Stream } from "./internal/webtransport/mod.ts";
import type { Reader } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import { Cond, Mutex, Once } from "@okdaichi/golikejs/sync";
import type { CancelCauseFunc, Context } from "@okdaichi/golikejs/context";
import { withCancelCause } from "@okdaichi/golikejs/context";
import { WebTransportStreamError } from "./internal/webtransport/mod.ts";
import type { SubscribeID, TrackPriority } from "./alias.ts";
import { SubscribeErrorCode } from "./error.ts";

/** Subscriber-side configuration sent in a SUBSCRIBE message. */
export interface TrackConfig {
	/** Subscriber priority for this track. */
	priority: TrackPriority;
	/** Whether the subscriber requires ordered delivery. */
	ordered: boolean;
	/** Maximum acceptable latency in milliseconds. */
	maxLatency: number;
	/** First group the subscriber wants to receive. */
	startGroup: number;
	/** Last group the subscriber wants to receive (0 = unbounded). */
	endGroup: number;
}

/** Notification that the publisher dropped a range of groups. */
export interface SubscribeDrop {
	/** First dropped group sequence. */
	startGroup: number;
	/** Last dropped group sequence. */
	endGroup: number;
	/** Reason code for the drop. */
	errorCode: number;
}

export const MESSAGE_TYPE_SUBSCRIBE_OK = 0x0;
export const MESSAGE_TYPE_SUBSCRIBE_END = 0x1;
export const MESSAGE_TYPE_SUBSCRIBE_DROP = 0x2;

function groupSequenceFromWire(v: number): number {
	if (v === 0) return 0;
	return v - 1;
}

function groupSequenceToWire(gs: number): number {
	if (gs === 0) return 0;
	return gs + 1;
}

/**
 * Subscriber-side view of a subscribe stream.
 *
 * Sends SUBSCRIBE_UPDATE messages and reads SUBSCRIBE_OK / SUBSCRIBE_DROP
 * responses from the publisher.
 */
export class SendSubscribeStream {
	#config: TrackConfig;
	#id: SubscribeID;
	#stream: Stream;
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	#mu: Mutex = new Mutex();
	#cond: Cond = new Cond(this.#mu);
	#drops: SubscribeDrop[] = [];
	#resolvedStart: number = 0;
	#okReceived: boolean = false;
	#endGroup: number = 0;
	#ended: boolean = false;

	constructor(
		sessCtx: Context,
		stream: Stream,
		subscribe: SubscribeMessage,
		ok?: SubscribeOkMessage,
		end?: SubscribeEndMessage,
	) {
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);
		this.#stream = stream;
		this.#config = {
			priority: subscribe.subscriberPriority,
			ordered: subscribe.subscriberOrdered !== 0,
			maxLatency: subscribe.subscriberMaxLatency,
			startGroup: groupSequenceFromWire(subscribe.groupStart),
			endGroup: groupSequenceFromWire(subscribe.groupEnd),
		};
		this.#id = subscribe.subscribeId;
		if (ok) {
			this.#resolvedStart = ok.group;
			this.#okReceived = true;
		}
		if (end) {
			this.#endGroup = end.group;
			this.#ended = true;
		}
	}

	get subscribeId(): SubscribeID {
		return this.#id;
	}

	get config(): TrackConfig {
		return this.#config;
	}

	/** The absolute start group resolved by SUBSCRIBE_OK (0 until received). */
	get resolvedStart(): number {
		return this.#resolvedStart;
	}

	/** Whether SUBSCRIBE_OK has been received. */
	get okReceived(): boolean {
		return this.#okReceived;
	}

	/** Whether the publisher signaled SUBSCRIBE_END. */
	get ended(): boolean {
		return this.#ended;
	}

	/** The last group that may be delivered, from SUBSCRIBE_END. */
	get endGroup(): number {
		return this.#endGroup;
	}

	appendDrop(drop: SubscribeDrop): void {
		this.#drops.push(drop);
		this.#cond.broadcast();
	}

	pendingDrops(): SubscribeDrop[] {
		const drops = this.#drops;
		this.#drops = [];
		return drops;
	}

	droppedSignal(): Promise<void> {
		return this.#cond.wait();
	}

	async readSubscribeResponses(): Promise<void> {
		while (true) {
			const [resp, err] = await readSubscribeResponse(this.#stream.readable);
			if (err) {
				return;
			}

			if (resp.ok) {
				this.#resolvedStart = resp.ok.group;
				this.#okReceived = true;
				continue;
			}

			if (resp.end) {
				this.#endGroup = resp.end.group;
				this.#ended = true;
				continue;
			}

			if (resp.drop) {
				// SUBSCRIBE_DROP carries plain absolute sequences.
				this.appendDrop({
					startGroup: resp.drop.groupStart,
					endGroup: resp.drop.groupEnd,
					errorCode: resp.drop.errorCode,
				});
			}
		}
	}

	async update(update: TrackConfig): Promise<Error | undefined> {
		const msg = new SubscribeUpdateMessage({
			subscriberPriority: update.priority,
			subscriberOrdered: update.ordered ? 1 : 0,
			subscriberMaxLatency: update.maxLatency,
			groupStart: groupSequenceToWire(update.startGroup),
			groupEnd: groupSequenceToWire(update.endGroup),
		});
		const err = await msg.encode(this.#stream.writable);
		if (err) {
			return new Error(`Failed to write subscribe update: ${err}`);
		}
		this.#config = update;

		return undefined;
	}

	async closeWithError(code: SubscribeErrorCode): Promise<void> {
		const err = new WebTransportStreamError({
			source: "stream",
			streamErrorCode: code,
		}, false);
		await this.#stream.writable.cancel(code);
		this.#cancelFunc(err);
	}
}

/**
 * Publisher-side view of a subscribe stream.
 *
 * Reads SUBSCRIBE / SUBSCRIBE_UPDATE from the subscriber and writes
 * SUBSCRIBE_OK / SUBSCRIBE_DROP responses.
 */
export class ReceiveSubscribeStream {
	readonly subscribeId: SubscribeID;
	#trackConfig: TrackConfig;
	#mu: Mutex = new Mutex();
	#cond: Cond = new Cond(this.#mu);
	#stream: Stream;
	#responseStarted: boolean = false;
	#endSent: boolean = false;
	#ensureOkOnce: Once = new Once();
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;

	constructor(
		sessCtx: Context,
		stream: Stream,
		subscribe: SubscribeMessage,
	) {
		this.#stream = stream;
		this.subscribeId = subscribe.subscribeId;
		this.#trackConfig = {
			priority: subscribe.subscriberPriority,
			ordered: subscribe.subscriberOrdered !== 0,
			maxLatency: subscribe.subscriberMaxLatency,
			startGroup: groupSequenceFromWire(subscribe.groupStart),
			endGroup: groupSequenceFromWire(subscribe.groupEnd),
		};
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);

		this.#handleUpdates();
	}

	async #handleUpdates(): Promise<void> {
		while (true) {
			const msg = new SubscribeUpdateMessage({});
			const err = await msg.decode(this.#stream.readable);
			if (err) {
				if (err instanceof EOFError) {
					console.error(
						`moq: error reading SUBSCRIBE_UPDATE message for subscribe ID: ${this.subscribeId}: ${err}`,
					);
				}
				return;
			}

			this.#trackConfig = {
				priority: msg.subscriberPriority,
				ordered: msg.subscriberOrdered !== 0,
				maxLatency: msg.subscriberMaxLatency,
				startGroup: groupSequenceFromWire(msg.groupStart),
				endGroup: groupSequenceFromWire(msg.groupEnd),
			};

			this.#cond.broadcast();
		}
	}

	get trackConfig(): TrackConfig {
		return this.#trackConfig;
	}

	async updated(): Promise<void> {
		return this.#cond.wait();
	}

	/**
	 * Write SUBSCRIBE_OK with the resolved absolute start group.
	 */
	async writeOk(group: number): Promise<Error | undefined> {
		const err = this.context.err();
		if (err !== undefined) {
			return err;
		}

		// Write type byte for SUBSCRIBE_OK
		const [, writeErr] = await writeVarint(this.#stream.writable, MESSAGE_TYPE_SUBSCRIBE_OK);
		if (writeErr) {
			return new Error(`moq: failed to write SUBSCRIBE_OK type: ${writeErr}`);
		}

		const msg = new SubscribeOkMessage({ group });

		const encErr = await msg.encode(this.#stream.writable);
		if (encErr) {
			return new Error(`moq: failed to encode SUBSCRIBE_OK message: ${encErr}`);
		}

		this.#responseStarted = true;

		return undefined;
	}

	/** Send SUBSCRIBE_OK exactly once; subsequent calls are no-ops. */
	async ensureOk(group: number): Promise<Error | undefined> {
		return await this.#ensureOkOnce.do(() => this.writeOk(group));
	}

	/**
	 * Send SUBSCRIBE_END with the last group that may be delivered. Per
	 * moq-lite-05, SUBSCRIBE_END without a preceding SUBSCRIBE_OK signals a
	 * track that ended with no matching groups.
	 */
	async writeEnd(group: number): Promise<Error | undefined> {
		if (this.#endSent) {
			return undefined;
		}
		const err = this.context.err();
		if (err !== undefined) {
			return err;
		}

		const [, typeErr] = await writeVarint(this.#stream.writable, MESSAGE_TYPE_SUBSCRIBE_END);
		if (typeErr) {
			return new Error(`moq: failed to write SUBSCRIBE_END type: ${typeErr}`);
		}

		const msg = new SubscribeEndMessage({ group });
		const encErr = await msg.encode(this.#stream.writable);
		if (encErr) {
			return new Error(`moq: failed to encode SUBSCRIBE_END message: ${encErr}`);
		}

		this.#endSent = true;

		return undefined;
	}

	async writeDrop(drop: SubscribeDrop): Promise<Error | undefined> {
		if (!this.#responseStarted) {
			// A leading range is dropped implicitly by SUBSCRIBE_OK: resolving
			// the start group past the dropped range makes an explicit
			// SUBSCRIBE_DROP unnecessary.
			return await this.ensureOk(drop.endGroup + 1);
		}

		// Write type byte for SUBSCRIBE_DROP
		const [, typeErr] = await writeVarint(this.#stream.writable, MESSAGE_TYPE_SUBSCRIBE_DROP);
		if (typeErr) {
			return new Error(`moq: failed to write SUBSCRIBE_DROP type: ${typeErr}`);
		}

		// SUBSCRIBE_DROP carries plain absolute sequences.
		const msg = new SubscribeDropMessage({
			groupStart: drop.startGroup,
			groupEnd: drop.endGroup,
			errorCode: drop.errorCode,
		});
		const err = await msg.encode(this.#stream.writable);
		if (err) {
			return new Error(`moq: failed to encode SUBSCRIBE_DROP message: ${err}`);
		}

		return undefined;
	}

	async close(): Promise<void> {
		if (this.context.err()) {
			return;
		}
		this.#cancelFunc(undefined);
		await this.#stream.writable.close();

		this.#cond.broadcast();
	}

	async closeWithError(code: SubscribeErrorCode): Promise<void> {
		if (this.context.err()) {
			return;
		}
		const cause = new WebTransportStreamError(
			{ source: "stream", streamErrorCode: code },
			false,
		);
		this.#cancelFunc(cause);
		await this.#stream.writable.cancel(code);
		await this.#stream.readable.cancel(code);

		this.#cond.broadcast();
	}
}

/**
 * One decoded publisher message from the Subscribe Stream; exactly one
 * field is set.
 */
export interface SubscribeResponse {
	ok?: SubscribeOkMessage;
	end?: SubscribeEndMessage;
	drop?: SubscribeDropMessage;
}

export async function readSubscribeResponse(
	r: Reader,
): Promise<[SubscribeResponse, undefined] | [SubscribeResponse, Error]> {
	// Read the type byte: 0x0 = SUBSCRIBE_OK, 0x1 = SUBSCRIBE_END, 0x2 = SUBSCRIBE_DROP
	const [msgType, , err] = await readVarint(r);
	if (err) {
		return [{}, err];
	}

	switch (msgType) {
		case MESSAGE_TYPE_SUBSCRIBE_OK: {
			const msg = new SubscribeOkMessage({});
			const decErr = await msg.decode(r);
			if (decErr) {
				return [{}, decErr];
			}
			return [{ ok: msg }, undefined];
		}
		case MESSAGE_TYPE_SUBSCRIBE_END: {
			const msg = new SubscribeEndMessage({});
			const decErr = await msg.decode(r);
			if (decErr) {
				return [{}, decErr];
			}
			return [{ end: msg }, undefined];
		}
		case MESSAGE_TYPE_SUBSCRIBE_DROP: {
			const msg = new SubscribeDropMessage({});
			const decErr = await msg.decode(r);
			if (decErr) {
				return [{}, decErr];
			}
			return [{ drop: msg }, undefined];
		}
		default:
			return [{}, new Error(`unexpected SUBSCRIBE response type: ${msgType}`)];
	}
}
