import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

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
		const e = new MessageEncoder();
		e.varint(this.bitrate);
		e.varint(this.rtt);
		const [, err] = await w.write(e.frame());
		return err;
	}

	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		const d = new MessageDecoder(buf);

		this.bitrate = d.varint();
		this.rtt = d.varint();

		if (!d.eof()) {
			return new Error("ProbeMessage: unexpected trailing bytes");
		}

		return undefined;
	}
}
