import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

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
		const e = new MessageEncoder();
		e.string(this.prefix);
		e.varint(this.excludeHop);
		const [, err] = await w.write(e.frame());
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

		const d = new MessageDecoder(buf);

		this.prefix = d.string();
		this.excludeHop = d.varint();

		return undefined;
	}
}
