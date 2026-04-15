import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { parseVarint, readFull, readVarint, varintLen, writeVarint } from "./message.ts";

export interface ProbeMessageInit {
	bitrate?: number;
	rtt?: number;
}

export class ProbeMessage {
	bitrate: number;
	rtt: number;

	constructor(init: ProbeMessageInit = {}) {
		this.bitrate = init.bitrate ?? 0;
		this.rtt = init.rtt ?? 0;
	}

	get len(): number {
		return varintLen(this.bitrate) + varintLen(this.rtt);
	}

	async encode(w: Writer): Promise<Error | undefined> {
		const msgLen = this.len;
		let err: Error | undefined;

		[, err] = await writeVarint(w, msgLen);
		if (err) return err;

		[, err] = await writeVarint(w, this.bitrate);
		if (err) return err;

		[, err] = await writeVarint(w, this.rtt);
		if (err) return err;

		return undefined;
	}

	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		let offset = 0;

		const [bitrate, n1] = parseVarint(buf, offset);
		this.bitrate = bitrate;
		offset += n1;

		const [rtt, n2] = parseVarint(buf, offset);
		this.rtt = rtt;
		offset += n2;

		if (offset !== buf.length) {
			return new Error("ProbeMessage: unexpected trailing bytes");
		}

		return undefined;
	}
}
