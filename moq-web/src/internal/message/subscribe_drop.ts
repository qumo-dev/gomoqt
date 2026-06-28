import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseVarint, readFull, readVarint } from "./message.ts";

export interface SubscribeDropMessageInit {
	startGroup?: number;
	endGroup?: number;
	errorCode?: number;
}

export class SubscribeDropMessage {
	startGroup: number;
	endGroup: number;
	errorCode: number;

	constructor(init: SubscribeDropMessageInit = {}) {
		this.startGroup = init.startGroup ?? 0;
		this.endGroup = init.endGroup ?? 0;
		this.errorCode = init.errorCode ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.varint(this.startGroup);
			e.varint(this.endGroup);
			e.varint(this.errorCode);
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
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		let offset = 0;

		[this.startGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.endGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.errorCode, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		return undefined;
	}
}
