import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { MessageDecoder, MessageEncoder, readFull, readVarint } from "./message.ts";

/**
 * Type tags for the announcements a publisher sends on the Announce Stream
 * after ANNOUNCE_OK (moq-lite-06 §7.6–7.8). Each message's `encode` writes its
 * own tag; its `decode` expects the caller to have consumed it.
 */
export const AnnounceMessageTypes = {
	Start: 0x0,
	End: 0x1,
	Update: 0x2,
} as const;

/** Raised for an announcement that does not parse: a malformed message. */
export class AnnounceDecodeError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "AnnounceDecodeError";
	}
}

/**
 * Returns the first non-zero Hop ID that appears twice in `ids`, or undefined.
 * Repeated zeros are legal: 0 identifies nothing (moq-lite-06 §7.6).
 */
export function duplicateHopID(ids: readonly number[]): number | undefined {
	for (let i = 0; i < ids.length; i++) {
		const id = ids[i]!;
		if (id === 0) continue;
		for (let j = i + 1; j < ids.length; j++) {
			if (ids[j] === id) return id;
		}
	}
	return undefined;
}

async function readBody(r: Reader): Promise<[MessageDecoder | undefined, Error | undefined]> {
	const [msgLen, , err1] = await readVarint(r);
	if (err1) return [undefined, err1];
	const buf = new Uint8Array(msgLen);
	const [, err2] = await readFull(r, buf);
	if (err2) return [undefined, err2];
	return [new MessageDecoder(buf), undefined];
}

/** Reads a varint from `d`, refusing one that runs past the body. */
function strictVarint(d: MessageDecoder): number {
	if (d.remaining() < 1) {
		throw new AnnounceDecodeError("message too short");
	}
	const v = d.varint();
	if (Number.isNaN(v) || d.remaining() < 0) {
		throw new AnnounceDecodeError("message too short");
	}
	return v;
}

function readHopList(d: MessageDecoder): number[] {
	const count = strictVarint(d);
	if (count > d.remaining()) {
		throw new AnnounceDecodeError("hop count exceeds message");
	}
	const ids: number[] = [];
	for (let i = 0; i < count; i++) {
		ids.push(strictVarint(d));
	}
	return ids;
}

function finish(d: MessageDecoder): void {
	if (!d.eof() || d.remaining() < 0) {
		throw new AnnounceDecodeError("message length mismatch");
	}
}

async function decodeWith(
	r: Reader,
	parse: (d: MessageDecoder) => void,
): Promise<Error | undefined> {
	const [d, err] = await readBody(r);
	if (err || d === undefined) return err;
	try {
		parse(d);
		finish(d);
	} catch (e) {
		if (e instanceof Error) return e;
		return new AnnounceDecodeError(String(e));
	}
	return undefined;
}

function writeHopList(e: MessageEncoder, ids: readonly number[]): void {
	e.varint(ids.length);
	for (const id of ids) e.varint(id);
}

/** Frames a typed message: Type varint, then the length-prefixed body. */
function typed(type: number, body: Uint8Array): Uint8Array {
	const out = new Uint8Array(1 + body.length);
	out[0] = type; // every assigned tag fits a one-byte varint
	out.set(body, 1);
	return out;
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

	/** Encodes the Type tag and the message in one write. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.string(this.suffix);
		writeHopList(e, this.hopIDs);
		e.varint(this.warmCost);
		e.varint(this.coldCost);
		const [, err] = await w.write(typed(AnnounceMessageTypes.Start, e.frame()));
		return err;
	}

	/** Decodes the message after its Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeWith(r, (d) => {
			if (d.remaining() < 1) throw new AnnounceDecodeError("message too short");
			this.suffix = d.string();
			if (d.remaining() < 0) throw new AnnounceDecodeError("message too short");
			this.hopIDs = readHopList(d);
			this.warmCost = strictVarint(d);
			this.coldCost = strictVarint(d);
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
	announceId: number;

	constructor(init: AnnounceEndMessageInit = {}) {
		this.announceId = init.announceId ?? 0;
	}

	/** Encodes the Type tag and the message in one write. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder(8);
		e.varint(this.announceId);
		const [, err] = await w.write(typed(AnnounceMessageTypes.End, e.frame()));
		return err;
	}

	/** Decodes the message after its Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeWith(r, (d) => {
			this.announceId = strictVarint(d);
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

	/** Encodes the Type tag and the message in one write. */
	async encode(w: Writer): Promise<Error | undefined> {
		const e = new MessageEncoder();
		e.varint(this.announceId);
		writeHopList(e, this.hopIDs);
		e.varint(this.warmCost);
		e.varint(this.coldCost);
		const [, err] = await w.write(typed(AnnounceMessageTypes.Update, e.frame()));
		return err;
	}

	/** Decodes the message after its Type tag. */
	decode(r: Reader): Promise<Error | undefined> {
		return decodeWith(r, (d) => {
			this.announceId = strictVarint(d);
			this.hopIDs = readHopList(d);
			this.warmCost = strictVarint(d);
			this.coldCost = strictVarint(d);
		});
	}
}
