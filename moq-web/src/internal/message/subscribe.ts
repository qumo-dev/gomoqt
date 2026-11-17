import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeMessageInit {
	subscribeId?: number;
	broadcastPath?: string;
	trackName?: string;
	subscriberPriority?: number;
	subscriberMaxAge?: number;
	groupStart?: number;
	groupEnd?: number;
	frameStart?: number;
	frameEnd?: number;
}

/**
 * SUBSCRIBE message.
 *
 * ```text
 * SUBSCRIBE Message {
 *   Message Length (i)
 *   Subscribe ID (i)
 *   Broadcast Path (s)
 *   Track Name (s)
 *   Subscriber Priority (8)
 *   Subscriber Max Age (i)
 *   Group Start (i)
 *   Group End (i)
 *   Frame Start (i)
 *   Frame End (i)
 * }
 * ```
 *
 * Group Start and Frame Start are plain (Group Start is a floor, 0 meaning
 * none); Group End and Frame End are absolute + 1, 0 meaning unbounded / the
 * whole group.
 */
export class SubscribeMessage {
	subscribeId: number;
	broadcastPath: string;
	trackName: string;
	subscriberPriority: number;
	subscriberMaxAge: number;
	groupStart: number;
	groupEnd: number;
	frameStart: number;
	frameEnd: number;

	constructor(init: SubscribeMessageInit = {}) {
		this.subscribeId = init.subscribeId ?? 0;
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
		this.subscriberPriority = init.subscriberPriority ?? 0;
		this.subscriberMaxAge = init.subscriberMaxAge ?? 0;
		this.groupStart = init.groupStart ?? 0;
		this.groupEnd = init.groupEnd ?? 0;
		this.frameStart = init.frameStart ?? 0;
		this.frameEnd = init.frameEnd ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.subscribeId);
		e.string(this.broadcastPath);
		e.string(this.trackName);
		e.uint8(this.subscriberPriority);
		e.varint(this.subscriberMaxAge);
		e.varint(this.groupStart);
		e.varint(this.groupEnd);
		e.varint(this.frameStart);
		e.varint(this.frameEnd);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/**
	 * Decodes the message from the reader.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		let err: Error | undefined;

		// Read message length
		let msgLen: number;
		[msgLen, , err] = await readVarint(r);
		if (err) return err;

		// Read message body into a buffer
		const buf = new Uint8Array(msgLen);
		[, err] = await readFull(r, buf);
		if (err) return err;

		// Parse fields from the buffer
		// Every field comes from the peer: a truncated one is an error, never a
		// NaN that reaches the publisher (Max Age feeds group expiry).
		const d = new MessageDecoder(buf);
		try {
			this.subscribeId = d.strictVarint();
			this.broadcastPath = d.strictString();
			this.trackName = d.strictString();
			this.subscriberPriority = d.strictUint8();
			this.subscriberMaxAge = d.strictVarint();
			this.groupStart = d.strictVarint();
			this.groupEnd = d.strictVarint();
			this.frameStart = d.strictVarint();
			this.frameEnd = d.strictVarint();
			d.finish();
		} catch (e) {
			return e instanceof Error ? e : new Error(String(e));
		}

		return undefined;
	}
}
