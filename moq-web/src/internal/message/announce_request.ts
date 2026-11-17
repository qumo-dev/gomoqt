import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface AnnounceRequestMessageInit {
	prefix?: string;
}

/**
 * ANNOUNCE_REQUEST message. moq-lite-06 removed Exclude Hop: the receiver's
 * hop-based loop check and the SETUP Hop parameter cover it.
 *
 * ```text
 * ANNOUNCE_REQUEST Message {
 *   Message Length (i)
 *   Broadcast Path Prefix (s),
 * }
 * ```
 */
export class AnnounceRequestMessage {
	prefix: string;

	constructor(init: AnnounceRequestMessageInit = {}) {
		this.prefix = init.prefix ?? "";
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.prefix);
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

		return undefined;
	}
}
