import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface GoawayMessageInit {
	newSessionURI?: string;
}

export class GoawayMessage {
	newSessionURI: string;

	constructor(init: GoawayMessageInit = {}) {
		this.newSessionURI = init.newSessionURI ?? "";
	}

	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.newSessionURI);
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
		this.newSessionURI = d.string();

		return undefined;
	}
}
