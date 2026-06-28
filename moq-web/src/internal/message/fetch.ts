import type { Reader, Writer } from "@okdaichi/golikejs/io";
import {
	MessageEncoder,
	parseString,
	parseUint8,
	parseVarint,
	readFull,
	readVarint,
} from "./message.ts";

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
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			e.string(this.broadcastPath);
			e.string(this.trackName);
			e.uint8(this.priority);
			e.varint(this.groupSequence);
			buf = e.frame();
		} catch (err) {
			return err as Error;
		}

		const [, err] = await w.write(buf);
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

		let offset = 0;

		[this.broadcastPath, offset] = (() => {
			const [str, n] = parseString(buf, offset);
			return [str, offset + n];
		})();

		[this.trackName, offset] = (() => {
			const [str, n] = parseString(buf, offset);
			return [str, offset + n];
		})();

		[this.priority, offset] = (() => {
			const [val, n] = parseUint8(buf, offset);
			return [val, offset + n];
		})();

		[this.groupSequence, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		if (offset !== msgLen) {
			return new Error("message length mismatch");
		}

		return undefined;
	}
}
