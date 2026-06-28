import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

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
		return MessageEncoder.encode(w, (e) => {
			// AnnounceStatus as varint: 0x0 (ENDED) or 0x1 (ACTIVE)
			e.varint(this.active ? 1 : 0);
			e.string(this.suffix);
			e.varint(this.hopIDs.length);
			for (const id of this.hopIDs) {
				e.varint(id);
			}
		});
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

		const d = new MessageDecoder(buf);

		// Read AnnounceStatus as varint
		this.active = d.varint() === 1;

		this.suffix = d.string();

		const hopCount = d.varint();
		this.hopIDs = [];
		for (let i = 0; i < hopCount; i++) {
			this.hopIDs.push(d.varint());
		}

		return undefined;
	}
}
