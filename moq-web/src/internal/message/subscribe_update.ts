import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeUpdateMessageInit {
	subscriberPriority?: number;
	subscriberOrdered?: number;
	subscriberMaxLatency?: number;
	startGroup?: number;
	endGroup?: number;
}

export class SubscribeUpdateMessage {
	subscriberPriority: number;
	subscriberOrdered: number;
	subscriberMaxLatency: number;
	startGroup: number;
	endGroup: number;

	constructor(init: SubscribeUpdateMessageInit = {}) {
		this.subscriberPriority = init.subscriberPriority ?? 0;
		this.subscriberOrdered = init.subscriberOrdered ?? 0;
		this.subscriberMaxLatency = init.subscriberMaxLatency ?? 0;
		this.startGroup = init.startGroup ?? 0;
		this.endGroup = init.endGroup ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.uint8(this.subscriberPriority);
		e.uint8(this.subscriberOrdered);
		e.varint(this.subscriberMaxLatency);
		e.varint(this.startGroup);
		e.varint(this.endGroup);
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
		this.subscriberOrdered = d.uint8();
		this.subscriberMaxLatency = d.varint();
		this.startGroup = d.varint();
		this.endGroup = d.varint();

		return undefined;
	}
}
