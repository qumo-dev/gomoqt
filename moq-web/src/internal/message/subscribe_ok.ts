import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeOkMessageInit {
	group?: number;
}

/**
 * SUBSCRIBE_OK message (type 0x0), confirming a subscription and resolving
 * its absolute start group. The type varint is written by the caller before
 * `encode` / consumed before `decode`.
 *
 * ```text
 * SUBSCRIBE_OK Message {
 *   Type (i) = 0x0
 *   Message Length (i)
 *   Group (i)
 * }
 * ```
 */
export class SubscribeOkMessage {
	/**
	 * Absolute sequence of the first group that will be delivered
	 * (plain absolute sequence, not the +1 form used in SUBSCRIBE).
	 */
	group: number;

	constructor(init: SubscribeOkMessageInit = {}) {
		this.group = init.group ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.group);
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
		this.group = d.varint();

		return undefined;
	}
}
