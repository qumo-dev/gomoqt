import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

export interface SubscribeRedirectMessageInit {
	connectUri?: string;
	broadcastPath?: string;
	trackName?: string;
}

/**
 * SUBSCRIBE_REDIRECT message (type 0x3), a gomoqt extension of moq-lite-05.
 * The publisher sends it in place of SUBSCRIBE_OK, as the only message on the
 * Subscribe Stream, to say where to subscribe instead. It is sent only to a
 * peer that advertised the Redirect Setup Parameter. The type varint is
 * written by the caller before `encode` / consumed before `decode`.
 *
 * ```text
 * SUBSCRIBE_REDIRECT Message {
 *   Type (i) = 0x3
 *   Message Length (i)
 *   Connect URI (s)
 *   Broadcast Path (s)
 *   Track Name (s)
 * }
 * ```
 */
export class SubscribeRedirectMessage {
	/** The session to subscribe on; empty means the current one. */
	connectUri: string;
	/** The broadcast to subscribe to instead. */
	broadcastPath: string;
	/** The track to subscribe to instead. */
	trackName: string;

	constructor(init: SubscribeRedirectMessageInit = {}) {
		this.connectUri = init.connectUri ?? "";
		this.broadcastPath = init.broadcastPath ?? "";
		this.trackName = init.trackName ?? "";
	}

	/** Encodes the message to the writer. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.connectUri);
		e.string(this.broadcastPath);
		e.string(this.trackName);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/**
	 * Decodes the message from the reader. A body shorter or longer than its
	 * three strings is an error.
	 */
	async decode(r: Reader): Promise<Error | undefined> {
		const [msgLen, , err1] = await readVarint(r);
		if (err1) return err1;

		const buf = new Uint8Array(msgLen);
		const [, err2] = await readFull(r, buf);
		if (err2) return err2;

		try {
			const d = new MessageDecoder(buf);
			const connectUri = d.string();
			const broadcastPath = d.string();
			const trackName = d.string();
			if (d.remaining() !== 0) {
				return new Error("moq: SUBSCRIBE_REDIRECT does not match its length");
			}
			this.connectUri = connectUri;
			this.broadcastPath = broadcastPath;
			this.trackName = trackName;
		} catch (e) {
			return e instanceof Error ? e : new Error(String(e));
		}
		return undefined;
	}
}
