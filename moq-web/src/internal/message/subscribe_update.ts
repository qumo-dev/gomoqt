import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeUpdateMessageInit {
	subscriberPriority?: number;
	subscriberMaxAge?: number;
	groupStart?: number;
	groupEnd?: number;
	frameStart?: number;
	frameEnd?: number;
}

/**
 * SUBSCRIBE_UPDATE message; its fields are encoded as in SUBSCRIBE.
 *
 * ```text
 * SUBSCRIBE_UPDATE Message {
 *   Message Length (i)
 *   Subscriber Priority (8)
 *   Subscriber Max Age (i)
 *   Group Start (i)
 *   Group End (i)
 *   Frame Start (i)
 *   Frame End (i)
 * }
 * ```
 */
export class SubscribeUpdateMessage {
	subscriberPriority: number;
	subscriberMaxAge: number;
	groupStart: number;
	groupEnd: number;
	frameStart: number;
	frameEnd: number;

	constructor(init: SubscribeUpdateMessageInit = {}) {
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

		let msgLen: number;
		[msgLen, , err] = await readVarint(r);
		if (err) return err;

		const buf = new Uint8Array(msgLen);
		[, err] = await readFull(r, buf);
		if (err) return err;

		const d = new MessageDecoder(buf);

		this.subscriberPriority = d.uint8();
		this.subscriberMaxAge = d.varint();
		this.groupStart = d.varint();
		this.groupEnd = d.varint();
		this.frameStart = d.varint();
		this.frameEnd = d.varint();

		return undefined;
	}
}
