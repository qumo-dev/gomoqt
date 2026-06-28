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
		return MessageEncoder.encode(w, (e) => {
			e.string(this.newSessionURI);
		});
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
