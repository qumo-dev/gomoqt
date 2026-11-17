import type { Reader, Writer } from "@okdaichi/golikejs/io";
import {
	MessageDecodeError,
	MessageDecoder,
	MessageEncoder,
	readFull,
	readVarint,
} from "./message.ts";

/**
 * Type tags for the announcements a publisher sends on the Announce Stream
 * after ANNOUNCE_OK (moq-lite-06 §7.6–7.8). Like the Subscribe Stream's
 * responses, the tag is written and read by the stream, not by the message:
 * `encode` writes and `decode` reads only the length-prefixed body.
 */
export const AnnounceMessageTypes = {
	Start: 0x0,
	End: 0x1,
	Update: 0x2,
} as const;

/**
 * Reads a length-prefixed body and parses it with `parse`, which reads fields
 * through the strict {@link MessageDecoder} methods. A body that does not
 * parse, or has bytes left over, yields a {@link MessageDecodeError}.
 */
async function decodeBody(
	r: Reader,
	parse: (d: MessageDecoder) => void,
): Promise<Error | undefined> {
	const [msgLen, , err1] = await readVarint(r);
	if (err1) return err1;
	const buf = new Uint8Array(msgLen);
	const [, err2] = await readFull(r, buf);
	if (err2) return err2;

	const d = new MessageDecoder(buf);
	try {
		parse(d);
		d.finish();
	} catch (e) {
		return e instanceof Error ? e : new MessageDecodeError(String(e));
	}
	return undefined;
}

function readHopList(d: MessageDecoder): number[] {
	const count = d.strictVarint();
	// Each Hop ID takes at least one byte; a larger count cannot fit the body.
	if (count > d.remaining()) {
		throw new MessageDecodeError("hop count exceeds message");
	}
	const ids: number[] = [];
	for (let i = 0; i < count; i++) {
		ids.push(d.strictVarint());
	}
	return ids;
}

function writeHopList(e: MessageEncoder, ids: readonly number[]): void {
	e.varint(ids.length);
	for (const id of ids) e.varint(id);
}

export interface AnnounceStartMessageInit {
	suffix?: string;
	hopIDs?: number[];
	warmCost?: number;
	coldCost?: number;
}

/**
 * ANNOUNCE_START: advertises a route, implicitly assigning the next Announce
 * ID on the stream.
 *
 * ```text
 * ANNOUNCE_START Message {
 *   Type (i) = 0x0
 *   Message Length (i)
 *   Route Prefix Suffix (s),
 *   Hop Count (i),
 *   Hop ID (i) ...,
 *   Warm Route Cost (i),
 *   Cold Route Cost (i),
 * }
 * ```
 */
export class AnnounceStartMessage {
	readonly type = AnnounceMessageTypes.Start;
	suffix: string;
	hopIDs: number[];
	warmCost: number;
	coldCost: number;

	constructor(init: AnnounceStartMessageInit = {}) {
		this.suffix = init.suffix ?? "";
		this.hopIDs = init.hopIDs ?? [];
		this.warmCost = init.warmCost ?? 0;
		this.coldCost = init.coldCost ?? 0;
	}

	/** Encodes the length-prefixed body; the caller writes the Type tag. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.suffix);
		writeHopList(e, this.hopIDs);
		e.varint(this.warmCost);
		e.varint(this.coldCost);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/** Decodes the length-prefixed body; the caller has read the Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeBody(r, (d) => {
			this.suffix = d.strictString();
			this.hopIDs = readHopList(d);
			this.warmCost = d.strictVarint();
			this.coldCost = d.strictVarint();
		});
	}
}

export interface AnnounceEndMessageInit {
	announceId?: number;
}

/**
 * ANNOUNCE_END: retracts a route started on the stream, by its Announce ID.
 *
 * ```text
 * ANNOUNCE_END Message {
 *   Type (i) = 0x1
 *   Message Length (i)
 *   Announce ID (i)
 * }
 * ```
 */
export class AnnounceEndMessage {
	readonly type = AnnounceMessageTypes.End;
	announceId: number;

	constructor(init: AnnounceEndMessageInit = {}) {
		this.announceId = init.announceId ?? 0;
	}

	/** Encodes the length-prefixed body; the caller writes the Type tag. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder(8);
		e.varint(this.announceId);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/** Decodes the length-prefixed body; the caller has read the Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeBody(r, (d) => {
			this.announceId = d.strictVarint();
		});
	}
}

export interface AnnounceUpdateMessageInit {
	announceId?: number;
	hopIDs?: number[];
	warmCost?: number;
	coldCost?: number;
}

/**
 * ANNOUNCE_UPDATE: replaces a started route's metadata in place, by its
 * Announce ID, which stays live.
 *
 * ```text
 * ANNOUNCE_UPDATE Message {
 *   Type (i) = 0x2
 *   Message Length (i)
 *   Announce ID (i),
 *   Hop Count (i),
 *   Hop ID (i) ...,
 *   Warm Route Cost (i),
 *   Cold Route Cost (i),
 * }
 * ```
 */
export class AnnounceUpdateMessage {
	readonly type = AnnounceMessageTypes.Update;
	announceId: number;
	hopIDs: number[];
	warmCost: number;
	coldCost: number;

	constructor(init: AnnounceUpdateMessageInit = {}) {
		this.announceId = init.announceId ?? 0;
		this.hopIDs = init.hopIDs ?? [];
		this.warmCost = init.warmCost ?? 0;
		this.coldCost = init.coldCost ?? 0;
	}

	/** Encodes the length-prefixed body; the caller writes the Type tag. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.announceId);
		writeHopList(e, this.hopIDs);
		e.varint(this.warmCost);
		e.varint(this.coldCost);
		const [, err] = await w.write(e.frame());
		return err;
	}

	/** Decodes the length-prefixed body; the caller has read the Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeBody(r, (d) => {
			this.announceId = d.strictVarint();
			this.hopIDs = readHopList(d);
			this.warmCost = d.strictVarint();
			this.coldCost = d.strictVarint();
		});
	}
}

/** An announcement the Announce Stream carries after ANNOUNCE_OK. */
export type AnnounceMessage =
	| AnnounceStartMessage
	| AnnounceEndMessage
	| AnnounceUpdateMessage;
