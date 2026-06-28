import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { encodeMessage, parseStringArray, readFull, readVarint } from "./message.ts";

export interface AnnounceInitMessageInit {
	suffixes?: string[];
}

export class AnnounceInitMessage {
	suffixes: string[];

	constructor(init: AnnounceInitMessageInit = {}) {
		this.suffixes = init.suffixes ?? [];
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		return encodeMessage(w, (e) => {
			e.stringArray(this.suffixes);
		});
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

		[this.suffixes] = parseStringArray(buf, 0);

		return undefined;
	}
}
