/**
 * Message encoding/decoding utilities.
 * Go-like design with io.Reader/io.Writer interfaces.
 */

import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import {
	bytesLen,
	MAX_VARINT1,
	MAX_VARINT2,
	MAX_VARINT4,
	MAX_VARINT8,
	stringLen,
	varintLen,
} from "../webtransport/len.ts";

export { bytesLen, stringLen, varintLen };

// Maximum bytes length (1 GiB)
export const MAX_BYTES_LENGTH = 1 << 30;

/**
 * Reads exactly len(p) bytes from r into p.
 * Like Go's io.ReadFull.
 */
export async function readFull(
	r: Reader,
	p: Uint8Array,
): Promise<[number, Error | undefined]> {
	let totalRead = 0;
	while (totalRead < p.length) {
		const [n, err] = await r.read(p.subarray(totalRead));
		totalRead += n;
		if (err) {
			return [totalRead, err];
		}
		if (n === 0) {
			return [totalRead, new EOFError()];
		}
	}
	return [totalRead, undefined];
}

// /**
//  * Writes a big-endian u16 to the writer.
//  * Returns number of bytes written and any error.
//  */
// export async function writeVarint(
// 	w: Writer,
// 	num: number,
// ): Promise<[number, Error | undefined]> {
// 	if (num < 0 || num > 0xffff) {
// 		return [0, new RangeError("Value exceeds u16 range")];
// 	}
// 	const buf = new Uint8Array(2);
// 	buf[0] = (num >> 8) & 0xff;
// 	buf[1] = num & 0xff;
// 	return await w.write(buf);
// }

// /**
//  * Reads a big-endian u16 from the reader.
//  * Returns the value, number of bytes read, and any error.
//  */
// export async function readVarint(
// 	r: Reader,
// ): Promise<[number, number, Error | undefined]> {
// 	const buf = new Uint8Array(2);
// 	const [n, err] = await readFull(r, buf);
// 	if (err) {
// 		return [0, n, err];
// 	}
// 	const value = (buf[0]! << 8) | buf[1]!;
// 	return [value, 2, undefined];
// }

/**
 * Writes a varint to the writer.
 * Returns number of bytes written and any error.
 */
export async function writeVarint(
	w: Writer,
	num: number,
): Promise<[number, Error | undefined]> {
	if (num < 0) {
		return [0, new Error("Varint cannot be negative")];
	}
	if (!Number.isFinite(num) || num > MAX_VARINT8) {
		return [0, new RangeError("Value exceeds maximum varint size")];
	}

	let buf: Uint8Array;
	if (num <= MAX_VARINT1) {
		buf = new Uint8Array([num]);
	} else if (num <= MAX_VARINT2) {
		buf = new Uint8Array(2);
		buf[0] = (num >> 8) | 0x40;
		buf[1] = num & 0xff;
	} else if (num <= MAX_VARINT4) {
		buf = new Uint8Array(4);
		buf[0] = (num >> 24) | 0x80;
		buf[1] = (num >> 16) & 0xff;
		buf[2] = (num >> 8) & 0xff;
		buf[3] = num & 0xff;
	} else {
		// JavaScript bitwise operations are limited to 32 bits,
		// so use division for shifts exceeding 32 bits
		buf = new Uint8Array(8);
		buf[0] = Math.floor(num / 0x100000000000000) | 0xc0;
		buf[1] = Math.floor(num / 0x1000000000000) & 0xff;
		buf[2] = Math.floor(num / 0x10000000000) & 0xff;
		buf[3] = Math.floor(num / 0x100000000) & 0xff;
		buf[4] = Math.floor(num / 0x1000000) & 0xff;
		buf[5] = Math.floor(num / 0x10000) & 0xff;
		buf[6] = Math.floor(num / 0x100) & 0xff;
		buf[7] = num & 0xff;
	}

	return await w.write(buf);
}

/**
 * Writes bytes with a varint length prefix to the writer.
 * Returns number of bytes written and any error.
 */
export async function writeBytes(
	w: Writer,
	data: Uint8Array,
): Promise<[number, Error | undefined]> {
	const [n1, err1] = await writeVarint(w, data.length);
	if (err1) {
		return [n1, err1];
	}
	const [n2, err2] = await w.write(data);
	return [n1 + n2, err2];
}

/**
 * Writes a string with a varint length prefix to the writer.
 * Returns number of bytes written and any error.
 */
export async function writeString(
	w: Writer,
	str: string,
): Promise<[number, Error | undefined]> {
	const encoder = new TextEncoder();
	const data = encoder.encode(str);
	return await writeBytes(w, data);
}

/**
 * Writes a string array to the writer.
 * Returns number of bytes written and any error.
 */
export async function writeStringArray(
	w: Writer,
	arr: string[],
): Promise<[number, Error | undefined]> {
	let total = 0;
	const [n1, err1] = await writeVarint(w, arr.length);
	if (err1) {
		return [n1, err1];
	}
	total += n1;

	for (const str of arr) {
		const [n, err] = await writeString(w, str);
		if (err) {
			return [total + n, err];
		}
		total += n;
	}

	return [total, undefined];
}

/**
 * Reads a varint from the reader.
 * Returns the value, number of bytes read, and any error.
 */
export async function readVarint(
	r: Reader,
): Promise<[number, number, Error | undefined]> {
	const firstByte = new Uint8Array(1);
	const [n, err] = await readFull(r, firstByte);
	if (err) {
		return [0, n, err];
	}

	const len = 1 << (firstByte[0]! >> 6);
	let value = firstByte[0]! & 0x3f;

	if (len === 1) {
		return [value, 1, undefined];
	}

	const remaining = new Uint8Array(len - 1);
	const [n2, err2] = await readFull(r, remaining);
	if (err2) {
		return [0, 1 + n2, err2];
	}

	// QUIC varint: multiply by 256 and add sequentially
	// JavaScript Number type can accurately represent up to 53 bits
	for (let i = 0; i < len - 1; i++) {
		value = value * 256 + remaining[i]!;
	}

	return [value, len, undefined];
}

/**
 * Reads bytes with a varint length prefix from the reader.
 * Returns the bytes, number of bytes read, and any error.
 */
export async function readBytes(
	r: Reader,
): Promise<[Uint8Array, number, Error | undefined]> {
	const [len, n1, err1] = await readVarint(r);
	if (err1) {
		return [new Uint8Array(0), n1, err1];
	}

	if (len > MAX_BYTES_LENGTH) {
		return [
			new Uint8Array(0),
			n1,
			new Error("Bytes length exceeds maximum limit"),
		];
	}

	const data = new Uint8Array(len);
	const [n2, err2] = await readFull(r, data);
	if (err2) {
		return [new Uint8Array(0), n1 + n2, err2];
	}

	return [data, n1 + n2, undefined];
}

/**
 * Reads a string with a varint length prefix from the reader.
 * Returns the string, number of bytes read, and any error.
 */
export async function readString(
	r: Reader,
): Promise<[string, number, Error | undefined]> {
	const [bytes, n, err] = await readBytes(r);
	if (err) {
		return ["", n, err];
	}
	const str = new TextDecoder().decode(bytes);
	return [str, n, undefined];
}

/**
 * Reads a string array from the reader.
 * Returns the array, number of bytes read, and any error.
 */
export async function readStringArray(
	r: Reader,
): Promise<[string[], number, Error | undefined]> {
	const [count, n1, err1] = await readVarint(r);
	if (err1) {
		return [[], n1, err1];
	}

	if (count > MAX_BYTES_LENGTH) {
		return [[], n1, new Error("String array count exceeds maximum limit")];
	}

	let total = n1;
	const arr: string[] = [];

	for (let i = 0; i < count; i++) {
		const [str, n, err] = await readString(r);
		if (err) {
			return [[], total + n, err];
		}
		arr.push(str);
		total += n;
	}

	return [arr, total, undefined];
}

// ============================================================
// Byte array parsing utilities (for parsing message body from buffer)
/**
 * Writes a single unsigned byte to the writer.
 * Returns number of bytes written and any error.
 */
export async function writeUint8(
	w: Writer,
	v: number,
): Promise<[number, Error | undefined]> {
	return await w.write(new Uint8Array([v & 0xff]));
}

/**
 * Parses a single unsigned byte from a byte array at the given offset.
 * Returns [value, bytesRead=1].
 */
export function parseUint8(buf: Uint8Array, offset: number): [number, number] {
	return [buf[offset]!, 1];
}

// ============================================================

/**
 * Parses a varint from a byte array at the given offset.
 * Returns [value, bytesRead].
 */
export function parseVarint(buf: Uint8Array, offset: number): [number, number] {
	const firstByte = buf[offset]!;
	const len = 1 << (firstByte >> 6);
	let value = firstByte & 0x3f;

	// QUIC varint: multiply by 256 and add sequentially
	// JavaScript Number type can accurately represent up to 53 bits
	for (let i = 1; i < len; i++) {
		value = value * 256 + buf[offset + i]!;
	}
	return [value, len];
}

/**
 * Parses bytes with a varint length prefix from a byte array.
 * Returns [bytes, bytesRead].
 */
export function parseBytes(
	buf: Uint8Array,
	offset: number,
): [Uint8Array, number] {
	const [len, n] = parseVarint(buf, offset);
	const bytes = buf.subarray(offset + n, offset + n + len);
	return [bytes, n + len];
}

/**
 * Parses a string with a varint length prefix from a byte array.
 * Returns [string, bytesRead].
 */
export function parseString(buf: Uint8Array, offset: number): [string, number] {
	const [bytes, n] = parseBytes(buf, offset);
	const str = new TextDecoder().decode(bytes);
	return [str, n];
}

/**
 * Parses a string array from a byte array.
 * Returns [array, bytesRead].
 */
export function parseStringArray(
	buf: Uint8Array,
	offset: number,
): [string[], number] {
	const [count, n1] = parseVarint(buf, offset);
	let total = n1;
	const arr: string[] = [];
	for (let i = 0; i < count; i++) {
		const [str, n] = parseString(buf, offset + total);
		arr.push(str);
		total += n;
	}
	return [arr, total];
}

// ============================================================
// Synchronous buffer encoding (mirror of the parse* readers).
//
// The async write* helpers above allocate a fresh Uint8Array per call and issue
// one `Writer.write` per field. `MessageEncoder` instead serializes a whole
// message body into a single growable buffer with sync writes, so a message
// encodes with one allocation and one `Writer.write` (see each message's
// `encode`). It also encodes strings to UTF-8 exactly once — unlike the
// `stringLen`-based sizing, which counts UTF-16 units.
// ============================================================

const utf8Encoder = new TextEncoder();

/**
 * Writes `num` as a QUIC varint into `buf` at `offset`.
 * Returns the offset just past the written bytes.
 * Throws on a negative or out-of-range value (matching {@link writeVarint}).
 */
export function putVarint(
	buf: Uint8Array,
	offset: number,
	num: number,
): number {
	if (num < 0) {
		throw new Error("Varint cannot be negative");
	}
	if (!Number.isFinite(num) || num > MAX_VARINT8) {
		throw new RangeError("Value exceeds maximum varint size");
	}

	if (num <= MAX_VARINT1) {
		buf[offset] = num;
		return offset + 1;
	} else if (num <= MAX_VARINT2) {
		buf[offset] = (num >> 8) | 0x40;
		buf[offset + 1] = num & 0xff;
		return offset + 2;
	} else if (num <= MAX_VARINT4) {
		buf[offset] = (num >> 24) | 0x80;
		buf[offset + 1] = (num >> 16) & 0xff;
		buf[offset + 2] = (num >> 8) & 0xff;
		buf[offset + 3] = num & 0xff;
		return offset + 4;
	} else {
		// JavaScript bitwise operations are limited to 32 bits,
		// so use division for shifts exceeding 32 bits.
		buf[offset] = Math.floor(num / 0x100000000000000) | 0xc0;
		buf[offset + 1] = Math.floor(num / 0x1000000000000) & 0xff;
		buf[offset + 2] = Math.floor(num / 0x10000000000) & 0xff;
		buf[offset + 3] = Math.floor(num / 0x100000000) & 0xff;
		buf[offset + 4] = Math.floor(num / 0x1000000) & 0xff;
		buf[offset + 5] = Math.floor(num / 0x10000) & 0xff;
		buf[offset + 6] = Math.floor(num / 0x100) & 0xff;
		buf[offset + 7] = num & 0xff;
		return offset + 8;
	}
}

/**
 * Accumulates a message body into a single growable buffer, then frames it with
 * a varint length prefix via {@link frame}. One allocation (plus occasional
 * doublings) and one `Writer.write` per message, instead of one of each per
 * field.
 */
export class MessageEncoder {
	// Reserve the maximum varint width (8 bytes) at the front so the body-length
	// prefix can be written in place by `frame()` with no extra copy.
	static readonly #PREFIX = 8;

	#buf: Uint8Array;
	#pos: number = MessageEncoder.#PREFIX;

	constructor(sizeHint: number = 64) {
		this.#buf = new Uint8Array(MessageEncoder.#PREFIX + sizeHint);
	}

	#ensure(extra: number): void {
		const need = this.#pos + extra;
		if (need <= this.#buf.length) {
			return;
		}
		let cap = this.#buf.length * 2;
		while (cap < need) {
			cap *= 2;
		}
		const grown = new Uint8Array(cap);
		grown.set(this.#buf.subarray(0, this.#pos));
		this.#buf = grown;
	}

	varint(num: number): void {
		this.#ensure(8); // maximum varint width
		this.#pos = putVarint(this.#buf, this.#pos, num);
	}

	uint8(v: number): void {
		this.#ensure(1);
		this.#buf[this.#pos++] = v & 0xff;
	}

	bytes(data: Uint8Array): void {
		this.varint(data.length);
		this.#ensure(data.length);
		this.#buf.set(data, this.#pos);
		this.#pos += data.length;
	}

	string(str: string): void {
		this.bytes(utf8Encoder.encode(str));
	}

	stringArray(arr: string[]): void {
		this.varint(arr.length);
		for (const str of arr) {
			this.string(str);
		}
	}

	/**
	 * Returns the encoded message: a varint length prefix followed by the body.
	 * The result is a view into the internal buffer — write it before reusing or
	 * discarding the encoder.
	 */
	frame(): Uint8Array {
		const bodyLen = this.#pos - MessageEncoder.#PREFIX;
		const width = varintLen(bodyLen);
		const start = MessageEncoder.#PREFIX - width;
		putVarint(this.#buf, start, bodyLen);
		return this.#buf.subarray(start, this.#pos);
	}

	/**
	 * Builds a message body with `build`, frames it, and writes it in a single
	 * `Writer.write`. Every message `encode` delegates here, so the encode error
	 * contract lives in one place: a `build` that throws (e.g. an out-of-range
	 * varint) is returned as an `Error` rather than rejecting.
	 */
	static async encode(
		w: Writer,
		build: (e: MessageEncoder) => void,
	): Promise<Error | undefined> {
		let buf: Uint8Array;
		try {
			const e = new MessageEncoder();
			build(e);
			buf = e.frame();
		} catch (err) {
			return err as Error;
		}

		const [, err] = await w.write(buf);
		return err;
	}
}

/**
 * Reads a message body sequentially from a buffer, owning the current offset so
 * callers read fields in order without threading it by hand. The synchronous
 * mirror of {@link MessageEncoder}: a message `decode` does the async work
 * (read the length prefix, read the body into one buffer), then walks the body
 * with a `MessageDecoder`. Each method delegates to the matching pure `parse*`
 * helper and advances the offset.
 */
export class MessageDecoder {
	#buf: Uint8Array;
	#offset: number = 0;

	constructor(buf: Uint8Array) {
		this.#buf = buf;
	}

	varint(): number {
		const [v, n] = parseVarint(this.#buf, this.#offset);
		this.#offset += n;
		return v;
	}

	uint8(): number {
		const [v, n] = parseUint8(this.#buf, this.#offset);
		this.#offset += n;
		return v;
	}

	string(): string {
		const [v, n] = parseString(this.#buf, this.#offset);
		this.#offset += n;
		return v;
	}

	stringArray(): string[] {
		const [v, n] = parseStringArray(this.#buf, this.#offset);
		this.#offset += n;
		return v;
	}

	/** Reads `length` raw bytes (no length prefix) as a view into the buffer. */
	bytes(length: number): Uint8Array {
		const out = this.#buf.subarray(this.#offset, this.#offset + length);
		this.#offset += length;
		return out;
	}

	/** Number of bytes left between the cursor and the end of the buffer. */
	remaining(): number {
		return this.#buf.length - this.#offset;
	}

	/** Whether the cursor has consumed the whole buffer. */
	eof(): boolean {
		return this.#offset >= this.#buf.length;
	}
}
