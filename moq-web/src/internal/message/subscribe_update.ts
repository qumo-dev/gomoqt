import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseUint8, parseVarint, readFull, readVarint } from "./message.ts";

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
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.uint8(this.subscriberPriority);
			e.uint8(this.subscriberOrdered);
			e.varint(this.subscriberMaxLatency);
			e.varint(this.startGroup);
			e.varint(this.endGroup);
			buf = e.frame();
		} catch (err) {
			return err as Error;
		}

		const [, err] = await w.write(buf);
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

		let offset = 0;

		[this.subscriberPriority, offset] = (() => {
			const [val, n] = parseUint8(buf, offset);
			return [val, offset + n];
		})();

		[this.subscriberOrdered, offset] = (() => {
			const [val, n] = parseUint8(buf, offset);
			return [val, offset + n];
		})();

		[this.subscriberMaxLatency, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.startGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.endGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		return undefined;
	}
}
