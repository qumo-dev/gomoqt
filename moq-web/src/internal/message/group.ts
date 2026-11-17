import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { decodeMessageBody, MessageEncoder } from "./message.ts";

export interface GroupMessageInit {
	subscribeId?: number;
	sequence?: number;
	frameStart?: number;
}

/**
 * GROUP message, opening a Group Stream.
 *
 * ```text
 * GROUP Message {
 *   Message Length (i)
 *   Subscribe ID (i)
 *   Group Sequence (i)
 *   Frame Start (i)
 * }
 * ```
 *
 * Frame Start is the index within the group of the first FRAME on the stream;
 * 0, the common case, means the stream carries the group from its beginning.
 */
export class GroupMessage {
	subscribeId: number;
	sequence: number;
	frameStart: number;

	constructor(init: GroupMessageInit = {}) {
		this.subscribeId = init.subscribeId ?? 0;
		this.sequence = init.sequence ?? 0;
		this.frameStart = init.frameStart ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.subscribeId);
		e.varint(this.sequence);
		e.varint(this.frameStart);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/**
	 * Decodes the message from the reader.
	 */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeMessageBody(r, (d) => {
			this.subscribeId = d.strictVarint();
			this.sequence = d.strictVarint();
			this.frameStart = d.strictVarint();
		});
	}
}
