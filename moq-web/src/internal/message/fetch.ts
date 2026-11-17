import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface FetchMessageInit {
	broadcastPath?: string;
	trackName?: string;
	priority?: number;
	groupSequence?: number;
	frameStart?: number;
	frameEnd?: number;
}

/**
 * FETCH message. Frame Start is a plain index; Frame End is the last index + 1,
 * 0 meaning through the end of the group.
 *
 * ```text
 * FETCH Message {
 *   Message Length (i)
 *   Broadcast Path (s)
 *   Track Name (s)
 *   Subscriber Priority (8)
 *   Group Sequence (i)
 *   Frame Start (i)
 *   Frame End (i)
 * }
 * ```
 */
export class FetchMessage {
	broadcastPath: string;
	trackName: string;
	priority: number;
	groupSequence: number;
	frameStart: number;
	frameEnd: number;

	constructor(init: FetchMessageInit = {}) {
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
		this.priority = init.priority ?? 0;
		this.groupSequence = init.groupSequence ?? 0;
		this.frameStart = init.frameStart ?? 0;
		this.frameEnd = init.frameEnd ?? 0;
	}

	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.broadcastPath);
		e.string(this.trackName);
		e.uint8(this.priority);
		e.varint(this.groupSequence);
		e.varint(this.frameStart);
		e.varint(this.frameEnd);
		const [, err] = await w.write(e.frame());
		return err;
	}

	async decode(r: Reader): Promise<Error | undefined> {
		let err: Error | undefined;

		let msgLen: number;
		[msgLen, , err] = await readVarint(r);
		if (err) return err;

		const buf = new Uint8Array(msgLen);
		[, err] = await readFull(r, buf);
		if (err) return err;

		const d = new MessageDecoder(buf);

		this.broadcastPath = d.string();
		this.trackName = d.string();
		this.priority = d.uint8();
		this.groupSequence = d.varint();
		this.frameStart = d.varint();
		this.frameEnd = d.varint();

		if (!d.eof()) {
			return new Error("message length mismatch");
		}

		return undefined;
	}
}
