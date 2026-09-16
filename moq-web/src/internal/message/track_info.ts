import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface TrackInfoMessageInit {
	publisherPriority?: number;
	publisherOrdered?: number;
	publisherMaxLatency?: number;
	timescale?: number;
}

/**
 * TRACK_INFO message, sent by the publisher as the sole response on a
 * Track Stream. Every field is fixed for the lifetime of the track;
 * Timescale MUST be non-zero.
 *
 * ```text
 * TRACK_INFO Message {
 *   Message Length (i)
 *   Publisher Priority (8)
 *   Publisher Ordered (8)
 *   Publisher Max Latency (i)
 *   Timescale (i)
 * }
 * ```
 */
export class TrackInfoMessage {
	publisherPriority: number;
	publisherOrdered: number;
	publisherMaxLatency: number;
	timescale: number;

	constructor(init: TrackInfoMessageInit = {}) {
		this.publisherPriority = init.publisherPriority ?? 0;
		this.publisherOrdered = init.publisherOrdered ?? 0;
		this.publisherMaxLatency = init.publisherMaxLatency ?? 0;
		this.timescale = init.timescale ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.uint8(this.publisherPriority);
		e.uint8(this.publisherOrdered);
		e.varint(this.publisherMaxLatency);
		e.varint(this.timescale);
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
		this.publisherPriority = d.uint8();
		this.publisherOrdered = d.uint8();
		this.publisherMaxLatency = d.varint();
		this.timescale = d.varint();

		return undefined;
	}
}
