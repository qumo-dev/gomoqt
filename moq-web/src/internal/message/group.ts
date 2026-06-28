import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseVarint, readFull, readVarint } from "./message.ts";

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
		return MessageEncoder.encode(w, (e) => {
			e.varint(this.subscribeId);
			e.varint(this.sequence);
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

		let offset = 0;

		[this.subscribeId, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.sequence] = parseVarint(buf, offset);

		return undefined;
	}
}
