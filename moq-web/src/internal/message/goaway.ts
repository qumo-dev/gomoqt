import type { Reader, Writer } from "@okdaichi/golikejs/io";
import {
	parseString,
	readFull,
	readVarint,
	stringLen,
	writeString,
	writeVarint,
} from "./message.ts";

export interface GoawayMessageInit {
	newSessionURI?: string;
}

export class GoawayMessage {
	newSessionURI: string;

	constructor(init: GoawayMessageInit = {}) {
		this.newSessionURI = init.newSessionURI ?? "";
	}

	get len(): number {
		return stringLen(this.newSessionURI);
	}

	async encode(w: Writer): Promise<Error | undefined> {
		const msgLen = this.len;
		let err: Error | undefined;

		[, err] = await writeVarint(w, msgLen);
		if (err) return err;

		[, err] = await writeString(w, this.newSessionURI);
		if (err) return err;

		return undefined;
	}

	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		[this.newSessionURI] = parseString(buf, 0);

		return undefined;
	}
}
