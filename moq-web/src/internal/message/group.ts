import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface GroupMessageInit {
	subscribeId?: number;
	sequence?: number;
}

export class GroupMessage {
	subscribeId: number;
	sequence: number;

	constructor(init: GroupMessageInit = {}) {
		this.subscribeId = init.subscribeId ?? 0;
		this.sequence = init.sequence ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.subscribeId);
		e.varint(this.sequence);
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
		this.subscribeId = d.varint();
		this.sequence = d.varint();

		return undefined;
	}
}
