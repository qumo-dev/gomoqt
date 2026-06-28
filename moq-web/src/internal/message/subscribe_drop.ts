import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeDropMessageInit {
	startGroup?: number;
	endGroup?: number;
	errorCode?: number;
}

export class SubscribeDropMessage {
	startGroup: number;
	endGroup: number;
	errorCode: number;

	constructor(init: SubscribeDropMessageInit = {}) {
		this.startGroup = init.startGroup ?? 0;
		this.endGroup = init.endGroup ?? 0;
		this.errorCode = init.errorCode ?? 0;
	}

	/**
	 * Encodes the message to the writer.
	 */
	async encode(w: Writer): Promise<Error | undefined> {
		return MessageEncoder.encode(w, (e) => {
			e.varint(this.startGroup);
			e.varint(this.endGroup);
			e.varint(this.errorCode);
		});
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

		this.startGroup = d.varint();
		this.endGroup = d.varint();
		this.errorCode = d.varint();

		return undefined;
	}
}
