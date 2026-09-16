import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface AnnounceOkMessageInit {
	hopID?: number;
	activeCount?: number;
}

/**
 * ANNOUNCE_OK message, sent exactly once as the first message on the
 * response side of an Announce Stream.
 *
 * ```text
 * ANNOUNCE_OK Message {
 *   Message Length (i)
 *   Hop ID (i)
 *   Active Count (i)
 * }
 * ```
 */
export class AnnounceOkMessage {
	/**
	 * The publisher's own Hop ID: the implicit trailing entry of every
	 * ANNOUNCE_BROADCAST's Hop ID list on this stream. 0 means unknown.
	 */
	hopID: number;
	/** Number of active ANNOUNCE_BROADCAST messages sent as the initial set. */
	activeCount: number;

	constructor(init: AnnounceOkMessageInit = {}) {
		this.hopID = init.hopID ?? 0;
		this.activeCount = init.activeCount ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.hopID);
		e.varint(this.activeCount);
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
		this.hopID = d.varint();
		this.activeCount = d.varint();

		return undefined;
	}
}
