import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageEncoder, parseString, parseVarint, readFull, readVarint } from "./message.ts";

export interface AnnounceMessageInit {
	suffix?: string;
	active?: boolean;
	hopIDs?: number[];
}

export class AnnounceMessage {
	suffix: string;
	active: boolean;
	hopIDs: number[];

	constructor(init: AnnounceMessageInit = {}) {
		this.suffix = init.suffix ?? "";
		this.active = init.active ?? false;
		this.hopIDs = init.hopIDs ?? [];
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			// AnnounceStatus as varint: 0x0 (ENDED) or 0x1 (ACTIVE)
			e.varint(this.active ? 1 : 0);
			e.string(this.suffix);
			e.varint(this.hopIDs.length);
			for (const id of this.hopIDs) {
				e.varint(id);
			}
			buf = e.frame();
		} catch (err) {
			return err as Error;
		}

		const [, err] = await w.write(buf);
		return err;
	}

	/**
	 * Decodes the message from the reader.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		let err: Error | undefined;

		let msgLen: number;
		[msgLen, , err] = await readVarint(r);
		if (err) return err;

		const buf = new Uint8Array(msgLen);
		[, err] = await readFull(r, buf);
		if (err) return err;

		let offset = 0;

		// Read AnnounceStatus as varint
		const [status, n1] = parseVarint(buf, offset);
		this.active = status === 1;
		offset += n1;

		[this.suffix, offset] = (() => {
			const [val, n] = parseString(buf, offset);
			return [val, offset + n];
		})();

		const [hopCount, n2] = parseVarint(buf, offset);
		offset += n2;

		this.hopIDs = [];
		for (let i = 0; i < hopCount; i++) {
			const [id, n] = parseVarint(buf, offset);
			this.hopIDs.push(id);
			offset += n;
		}

		return undefined;
	}
}
