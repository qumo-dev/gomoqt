import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface FetchMessageInit {
	broadcastPath?: string;
	trackName?: string;
	priority?: number;
	groupSequence?: number;
}

export class FetchMessage {
	broadcastPath: string;
	trackName: string;
	priority: number;
	groupSequence: number;

	constructor(init: FetchMessageInit = {}) {
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
		this.priority = init.priority ?? 0;
		this.groupSequence = init.groupSequence ?? 0;
	}

	async encode(w: Writer): Promise<Error | undefined> {
		return MessageEncoder.encode(w, (e) => {
			e.string(this.broadcastPath);
			e.string(this.trackName);
			e.uint8(this.priority);
			e.varint(this.groupSequence);
		});
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

		if (!d.eof()) {
			return new Error("message length mismatch");
		}

		return undefined;
	}
}
