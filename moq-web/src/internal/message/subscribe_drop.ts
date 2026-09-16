import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeDropMessageInit {
	groupStart?: number;
	groupEnd?: number;
	errorCode?: number;
}

export class SubscribeDropMessage {
	groupStart: number;
	groupEnd: number;
	errorCode: number;

	constructor(init: SubscribeDropMessageInit = {}) {
		this.groupStart = init.groupStart ?? 0;
		this.groupEnd = init.groupEnd ?? 0;
		this.errorCode = init.errorCode ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.groupStart);
		e.varint(this.groupEnd);
		e.varint(this.errorCode);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/**
	 * Decodes the message from the reader.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		const d = new MessageDecoder(buf);

		this.groupStart = d.varint();
		this.groupEnd = d.varint();
		this.errorCode = d.varint();

		return undefined;
	}
}
