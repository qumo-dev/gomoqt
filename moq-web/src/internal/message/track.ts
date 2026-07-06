import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface TrackMessageInit {
	broadcastPath?: string;
	trackName?: string;
}

/**
 * TRACK message, sent by a subscriber as the first message on a Track
 * Stream to request a track's immutable publisher properties.
 *
 * ```text
 * TRACK Message {
 *   Message Length (i)
 *   Broadcast Path (s)
 *   Track Name (s)
 * }
 * ```
 */
export class TrackMessage {
	broadcastPath: string;
	trackName: string;

	constructor(init: TrackMessageInit = {}) {
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.broadcastPath);
		e.string(this.trackName);
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
		this.broadcastPath = d.string();
		this.trackName = d.string();

		return undefined;
	}
}
