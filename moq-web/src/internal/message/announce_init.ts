import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseStringArray, readFull, readVarint } from "./message.ts";

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
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.stringArray(this.suffixes);
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

		[this.suffixes] = parseStringArray(buf, 0);

		return undefined;
	}
}
