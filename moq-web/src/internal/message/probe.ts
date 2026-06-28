import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseVarint, readFull, readVarint } from "./message.ts";

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

	async encode(w: Writer): Promise<Error | undefined> {
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.varint(this.bitrate);
			e.varint(this.rtt);
			buf = e.frame();
		} catch (err) {
			return err as Error;
		}

		const [, err] = await w.write(buf);
		return err;
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
