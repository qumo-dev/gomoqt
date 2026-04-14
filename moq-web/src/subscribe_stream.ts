import type { SubscribeMessage } from "./internal/message/mod.ts";
import {
	readVarint,
	SubscribeDropMessage,
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
import type { Info } from "./info.ts";
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

const MESSAGE_TYPE_SUBSCRIBE_OK = 0x0;
const MESSAGE_TYPE_SUBSCRIBE_DROP = 0x1;

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
	#info: Info;
	#stream: Stream;
	readonly context: Context;
	#cancelFunc: CancelCauseFunc;
	#mu: Mutex = new Mutex();
	#cond: Cond = new Cond(this.#mu);
	#drops: SubscribeDrop[] = [];

	constructor(
		sessCtx: Context,
		stream: Stream,
		subscribe: SubscribeMessage,
		ok: SubscribeOkMessage,
	) {
		[this.context, this.#cancelFunc] = withCancelCause(sessCtx);
		this.#stream = stream;
		this.#config = {
			priority: subscribe.subscriberPriority,
			ordered: subscribe.subscriberOrdered !== 0,
			maxLatency: subscribe.subscriberMaxLatency,
			startGroup: groupSequenceFromWire(subscribe.startGroup),
			endGroup: groupSequenceFromWire(subscribe.endGroup),
		};
		this.#id = subscribe.subscribeId;
		this.#info = {
			priority: ok.publisherPriority,
			ordered: ok.publisherOrdered !== 0,
			maxLatency: ok.publisherMaxLatency,
			startGroup: groupSequenceFromWire(ok.startGroup),
			endGroup: groupSequenceFromWire(ok.endGroup),
		};
	}

	get subscribeId(): SubscribeID {
		return this.#id;
	}

	get config(): TrackConfig {
		return this.#config;
	}

	get info(): Info {
		return this.#info;
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
			const [ok, drop, err] = await readSubscribeResponse(this.#stream.readable);
			if (err) {
				return;
			}

			if (ok) {
				// Update info (SubscribeOk can be received multiple times)
				this.#info = {
					priority: ok.publisherPriority,
					ordered: ok.publisherOrdered !== 0,
					maxLatency: ok.publisherMaxLatency,
					startGroup: groupSequenceFromWire(ok.startGroup),
					endGroup: groupSequenceFromWire(ok.endGroup),
				};
				continue;
			}

			if (drop) {
				this.appendDrop({
					startGroup: drop.startGroup,
					endGroup: drop.endGroup,
					errorCode: drop.errorCode,
				});
				return;
			}
		}
	}

	async update(update: TrackConfig): Promise<Error | undefined> {
		const msg = new SubscribeUpdateMessage({
			subscriberPriority: update.priority,
			subscriberOrdered: update.ordered ? 1 : 0,
			subscriberMaxLatency: update.maxLatency,
			startGroup: groupSequenceToWire(update.startGroup),
			endGroup: groupSequenceToWire(update.endGroup),
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
	#ensureInfoOnce: Once = new Once();
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
			startGroup: groupSequenceFromWire(subscribe.startGroup),
			endGroup: groupSequenceFromWire(subscribe.endGroup),
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
				startGroup: groupSequenceFromWire(msg.startGroup),
				endGroup: groupSequenceFromWire(msg.endGroup),
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

	async writeInfo(info?: Info): Promise<Error | undefined> {
		const err = this.context.err();
		if (err !== undefined) {
			return err;
		}

		const i = info ??
			{ priority: 0, ordered: false, maxLatency: 0, startGroup: 0, endGroup: 0 };
		// Write type byte for SUBSCRIBE_OK
		const [, writeErr] = await writeVarint(this.#stream.writable, MESSAGE_TYPE_SUBSCRIBE_OK);
		if (writeErr) {
			return new Error(`moq: failed to write SUBSCRIBE_OK type: ${writeErr}`);
		}

		const msg = new SubscribeOkMessage({
			publisherPriority: i.priority,
			publisherOrdered: i.ordered ? 1 : 0,
			publisherMaxLatency: i.maxLatency,
			startGroup: groupSequenceToWire(i.startGroup),
			endGroup: groupSequenceToWire(i.endGroup),
		});

		const encErr = await msg.encode(this.#stream.writable);
		if (encErr) {
			return new Error(`moq: failed to encode SUBSCRIBE_OK message: ${encErr}`);
		}

		this.#responseStarted = true;

		return undefined;
	}

	async ensureInfo(info?: Info): Promise<Error | undefined> {
		return await this.#ensureInfoOnce.do(() => this.writeInfo(info));
	}

	async writeDrop(drop: SubscribeDrop): Promise<Error | undefined> {
		if (!this.#responseStarted) {
			const err = await this.ensureInfo();
			if (err) {
				return err;
			}
		}

		// Write type byte for SUBSCRIBE_DROP
		const [, typeErr] = await writeVarint(this.#stream.writable, MESSAGE_TYPE_SUBSCRIBE_DROP);
		if (typeErr) {
			return new Error(`moq: failed to write SUBSCRIBE_DROP type: ${typeErr}`);
		}

		const msg = new SubscribeDropMessage({
			startGroup: drop.startGroup,
			endGroup: drop.endGroup,
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

async function readSubscribeResponse(
	r: Reader,
): Promise<
	| [SubscribeOkMessage, undefined, undefined]
	| [undefined, SubscribeDropMessage, undefined]
	| [undefined, undefined, Error]
> {
	// Read the type byte: 0x0 = SUBSCRIBE_OK, 0x1 = SUBSCRIBE_DROP
	const [msgType, , err] = await readVarint(r);
	if (err) {
		return [undefined, undefined, err];
	}

	switch (msgType) {
		case MESSAGE_TYPE_SUBSCRIBE_OK: {
			const msg = new SubscribeOkMessage({});
			const decErr = await msg.decode(r);
			if (decErr) {
				return [undefined, undefined, decErr];
			}
			return [msg, undefined, undefined];
		}
		case MESSAGE_TYPE_SUBSCRIBE_DROP: {
			const msg = new SubscribeDropMessage({});
			const decErr = await msg.decode(r);
			if (decErr) {
				return [undefined, undefined, decErr];
			}
			return [undefined, msg, undefined];
		}
		default:
			return [
				undefined,
				undefined,
				new Error(`unexpected SUBSCRIBE response type: ${msgType}`),
			];
	}
}
