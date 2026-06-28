import type { Reader, Writer } from "@okdaichi/golikejs/io";
import {
	encodeMessage,
	parseString,
	parseUint8,
	parseVarint,
	readFull,
	readVarint,
} from "./message.ts";

export interface SubscribeMessageInit {
	subscribeId?: number;
	broadcastPath?: string;
	trackName?: string;
	subscriberPriority?: number;
	subscriberOrdered?: number;
	subscriberMaxLatency?: number;
	startGroup?: number;
	endGroup?: number;
}

export class SubscribeMessage {
	subscribeId: number;
	broadcastPath: string;
	trackName: string;
	subscriberPriority: number;
	subscriberOrdered: number;
	subscriberMaxLatency: number;
	startGroup: number;
	endGroup: number;

	constructor(init: SubscribeMessageInit = {}) {
		this.subscribeId = init.subscribeId ?? 0;
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
		this.subscriberPriority = init.subscriberPriority ?? 0;
		this.subscriberOrdered = init.subscriberOrdered ?? 0;
		this.subscriberMaxLatency = init.subscriberMaxLatency ?? 0;
		this.startGroup = init.startGroup ?? 0;
		this.endGroup = init.endGroup ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		return encodeMessage(w, (e) => {
			e.varint(this.subscribeId);
			e.string(this.broadcastPath);
			e.string(this.trackName);
			e.uint8(this.subscriberPriority);
			e.uint8(this.subscriberOrdered);
			e.varint(this.subscriberMaxLatency);
			e.varint(this.startGroup);
			e.varint(this.endGroup);
		});
	}

	/**
	 * Decodes the message from the reader.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		let err: Error | undefined;

		// Read message length
		let msgLen: number;
		[msgLen, , err] = await readVarint(r);
		if (err) return err;

		// Read message body into a buffer
		const buf = new Uint8Array(msgLen);
		[, err] = await readFull(r, buf);
		if (err) return err;

		// Parse fields from the buffer
		let offset = 0;

		[this.subscribeId, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.broadcastPath, offset] = (() => {
			const [val, n] = parseString(buf, offset);
			return [val, offset + n];
		})();

		[this.trackName, offset] = (() => {
			const [val, n] = parseString(buf, offset);
			return [val, offset + n];
		})();

		[this.subscriberPriority, offset] = (() => {
			const [val, n] = parseUint8(buf, offset);
			return [val, offset + n];
		})();

		[this.subscriberOrdered, offset] = (() => {
			const [val, n] = parseUint8(buf, offset);
			return [val, offset + n];
		})();

		[this.subscriberMaxLatency, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.startGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		[this.endGroup, offset] = (() => {
			const [val, n] = parseVarint(buf, offset);
			return [val, offset + n];
		})();

		return undefined;
	}
}
