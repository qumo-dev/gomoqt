import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeOkMessageInit {
	publisherPriority?: number;
	publisherOrdered?: number;
	publisherMaxLatency?: number;
	startGroup?: number;
	endGroup?: number;
}

export class SubscribeOkMessage {
	publisherPriority: number;
	publisherOrdered: number;
	publisherMaxLatency: number;
	startGroup: number;
	endGroup: number;

	constructor(init: SubscribeOkMessageInit = {}) {
		this.publisherPriority = init.publisherPriority ?? 0;
		this.publisherOrdered = init.publisherOrdered ?? 0;
		this.publisherMaxLatency = init.publisherMaxLatency ?? 0;
		this.startGroup = init.startGroup ?? 0;
		this.endGroup = init.endGroup ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		return MessageEncoder.encode(w, (e) => {
			e.uint8(this.publisherPriority);
			e.uint8(this.publisherOrdered);
			e.varint(this.publisherMaxLatency);
			e.varint(this.startGroup);
			e.varint(this.endGroup);
		});
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

		this.publisherPriority = d.uint8();
		this.publisherOrdered = d.uint8();
		this.publisherMaxLatency = d.varint();
		this.startGroup = d.varint();
		this.endGroup = d.varint();

		return undefined;
	}
}
