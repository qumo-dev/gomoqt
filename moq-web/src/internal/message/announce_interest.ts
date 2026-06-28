import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseString, parseVarint, readFull, readVarint } from "./message.ts";

export interface AnnounceInterestMessageInit {
	prefix?: string;
	excludeHop?: number;
}

export class AnnounceInterestMessage {
	prefix: string;
	excludeHop: number;

	constructor(init: AnnounceInterestMessageInit = {}) {
		this.prefix = init.prefix ?? "";
		this.excludeHop = init.excludeHop ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.string(this.prefix);
			e.varint(this.excludeHop);
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

		[this.prefix, offset] = (() => {
			const [val, n] = parseString(buf, offset);
			return [val, offset + n];
		})();

		const [excludeHop] = parseVarint(buf, offset);
		this.excludeHop = excludeHop;

		return undefined;
	}
}
