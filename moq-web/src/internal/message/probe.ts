import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { parseVarint, readFull, readVarint, varintLen, writeVarint } from "./message.ts";

export interface ProbeMessageInit {
	bitrate?: number;
}

export class ProbeMessage {
	bitrate: number;

	constructor(init: ProbeMessageInit = {}) {
		this.bitrate = init.bitrate ?? 0;
	}

	get len(): number {
		return varintLen(this.bitrate);
	}

	async encode(w: Writer): Promise<Error | undefined> {
		const msgLen = this.len;
		let err: Error | undefined;

		[, err] = await writeVarint(w, msgLen);
		if (err) return err;

		[, err] = await writeVarint(w, this.bitrate);
		if (err) return err;

		return undefined;
	}

	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		const [bitrate, n1] = parseVarint(buf, 0);
		this.bitrate = bitrate;

		if (n1 !== buf.length) {
			return new Error("ProbeMessage: unexpected trailing bytes");
		}

		return undefined;
	}
}
