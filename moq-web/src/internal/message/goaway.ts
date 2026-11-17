import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";
import { varintLen } from "../webtransport/len.ts";

/** The longest GOAWAY New Session URI, in bytes (moq-lite-06 §7.18). */
export const MAX_NEW_SESSION_URI_LEN = 8192;

/** Raised for a GOAWAY New Session URI longer than {@link MAX_NEW_SESSION_URI_LEN}. */
export class NewSessionURITooLongError extends Error {
	constructor() {
		super("moq: GOAWAY new session URI too long");
		this.name = "NewSessionURITooLongError";
	}
}

export interface GoawayMessageInit {
	newSessionURI?: string;
}

/**
 * GOAWAY message.
 *
 * ```text
 * GOAWAY Message {
 *   Message Length (i)
 *   New Session URI (s)
 * }
 * ```
 */
export class GoawayMessage {
	newSessionURI: string;

	constructor(init: GoawayMessageInit = {}) {
		this.newSessionURI = init.newSessionURI ?? "";
	}

	/** Encodes the message, refusing a URI over the limit. */
	async encode(w: Writer): Promise<Error | undefined> {
		const uri = new TextEncoder().encode(this.newSessionURI);
		if (uri.length > MAX_NEW_SESSION_URI_LEN) {
			return new NewSessionURITooLongError();
		}
		const e = new MessageEncoder();
		e.bytes(uri);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/** Decodes the message; an over-long URI is refused from the length alone. */
	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;
		if (msgLen > MAX_NEW_SESSION_URI_LEN + varintLen(MAX_NEW_SESSION_URI_LEN)) {
			return new NewSessionURITooLongError();
		}

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		const d = new MessageDecoder(buf);
		const uriLen = d.varint();
		if (uriLen > MAX_NEW_SESSION_URI_LEN) {
			return new NewSessionURITooLongError();
		}
		this.newSessionURI = new TextDecoder().decode(d.bytes(uriLen));

		return undefined;
	}
}
