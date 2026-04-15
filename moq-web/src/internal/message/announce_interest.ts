import type { Reader, Writer } from "@okdaichi/golikejs/io";
import {
	parseString,
	parseVarint,
	readFull,
	readVarint,
	stringLen,
	varintLen,
	writeString,
	writeVarint,
} from "./message.ts";

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
	 * Returns the length of the message body (excluding the length prefix).
	 */
	get len(): number {
		return stringLen(this.prefix) + varintLen(this.excludeHop);
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const msgLen = this.len;
		let err: Error | undefined;

		[, err] = await writeVarint(w, msgLen);
		if (err) return err;

		[, err] = await writeString(w, this.prefix);
		if (err) return err;

		[, err] = await writeVarint(w, this.excludeHop);
		if (err) return err;

		return undefined;
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
