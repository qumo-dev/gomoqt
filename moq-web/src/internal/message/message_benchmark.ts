/**
 * Benchmarks for the message codec hot path.
 *
 * This is the serialization layer every stream goes through: varint
 * encode/decode, length-prefixed strings, and the per-message
 * `encode`/`decode` round trips. To isolate codec CPU from WebTransport
 * stream and async-scheduling overhead, the benchmarks use minimal
 * in-memory {@link Reader}/{@link Writer} implementations rather than the
 * mock streams (which wrap every call in a `spy`).
 *
 * Run with:
 *   deno bench --allow-all src/internal/message/message_benchmark.ts
 */

import type { Reader, Writer } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import { parseVarint, readVarint, writeString, writeStringArray, writeVarint } from "./message.ts";
import { GroupMessage } from "./group.ts";
import { SubscribeMessage } from "./subscribe.ts";

/**
 * Minimal in-memory {@link Writer} that appends every write into a single
 * growing buffer. No allocation per write beyond the occasional grow.
 */
class BufWriter implements Writer {
	#buf: Uint8Array;
	#len = 0;

	constructor(capacity = 1024) {
		this.#buf = new Uint8Array(capacity);
	}

	get bytes(): Uint8Array {
		return this.#buf.subarray(0, this.#len);
	}

	reset(): void {
		this.#len = 0;
	}

	// deno-lint-ignore require-await
	async write(p: Uint8Array): Promise<[number, Error | undefined]> {
		const need = this.#len + p.length;
		if (need > this.#buf.length) {
			let cap = this.#buf.length * 2;
			while (cap < need) cap *= 2;
			const grown = new Uint8Array(cap);
			grown.set(this.#buf.subarray(0, this.#len));
			this.#buf = grown;
		}
		this.#buf.set(p, this.#len);
		this.#len += p.length;
		return [p.length, undefined];
	}
}

/**
 * Minimal in-memory {@link Reader} that serves bytes from a fixed buffer
 * and can be rewound to the start, so a single instance can be reused
 * across benchmark iterations without re-encoding.
 */
class BufReader implements Reader {
	#buf: Uint8Array;
	#pos = 0;

	constructor(buf: Uint8Array) {
		this.#buf = buf;
	}

	rewind(): void {
		this.#pos = 0;
	}

	// deno-lint-ignore require-await
	async read(p: Uint8Array): Promise<[number, Error | undefined]> {
		if (this.#pos >= this.#buf.length) {
			return [0, new EOFError()];
		}
		const n = Math.min(p.length, this.#buf.length - this.#pos);
		p.set(this.#buf.subarray(this.#pos, this.#pos + n));
		this.#pos += n;
		return [n, undefined];
	}
}

// Representative values for each varint width.
const VARINT_VALUES: Record<string, number> = {
	"1-byte (63)": 63,
	"2-byte (16383)": 16383,
	"4-byte (1073741823)": 1073741823,
	"8-byte (2^48)": 2 ** 48,
};

// ---------------------------------------------------------------------------
// writeVarint: encode cost across widths
// ---------------------------------------------------------------------------

for (const [label, value] of Object.entries(VARINT_VALUES)) {
	const w = new BufWriter(16);
	Deno.bench({
		name: `writeVarint ${label}`,
		group: "writeVarint",
		baseline: label.startsWith("1-byte"),
		fn: async () => {
			w.reset();
			await writeVarint(w, value);
		},
	});
}

// ---------------------------------------------------------------------------
// readVarint (async, stream path) vs parseVarint (sync, buffer path)
// ---------------------------------------------------------------------------

for (const [label, value] of Object.entries(VARINT_VALUES)) {
	const enc = new BufWriter(16);
	await writeVarint(enc, value);
	const encoded = enc.bytes.slice();
	const r = new BufReader(encoded);

	Deno.bench({
		name: `readVarint ${label}`,
		group: "readVarint",
		baseline: label.startsWith("1-byte"),
		fn: async () => {
			r.rewind();
			await readVarint(r);
		},
	});

	Deno.bench({
		name: `parseVarint ${label}`,
		group: "parseVarint",
		baseline: label.startsWith("1-byte"),
		fn: () => {
			parseVarint(encoded, 0);
		},
	});
}

// ---------------------------------------------------------------------------
// Length-prefixed string and string-array encode
// ---------------------------------------------------------------------------

const SHORT_PATH = "/live/alice/camera";
const LONG_PATH = "/" + "segment/".repeat(16) + "track.mp4";

{
	const w = new BufWriter(256);
	Deno.bench({
		name: "writeString short path",
		group: "writeString",
		baseline: true,
		fn: async () => {
			w.reset();
			await writeString(w, SHORT_PATH);
		},
	});
}

{
	const w = new BufWriter(512);
	Deno.bench({
		name: "writeString long path",
		group: "writeString",
		fn: async () => {
			w.reset();
			await writeString(w, LONG_PATH);
		},
	});
}

{
	const w = new BufWriter(512);
	const arr = ["/live/a", "/live/b", "/live/c", "/live/d"];
	Deno.bench({
		name: "writeStringArray (4 paths)",
		group: "writeStringArray",
		baseline: true,
		fn: async () => {
			w.reset();
			await writeStringArray(w, arr);
		},
	});
}

// ---------------------------------------------------------------------------
// Per-message encode / decode round trips
// ---------------------------------------------------------------------------

// GroupMessage: the most frequent control message (one per group).
{
	const w = new BufWriter(64);
	const msg = new GroupMessage({ subscribeId: 7, sequence: 123456 });
	Deno.bench({
		name: "GroupMessage.encode",
		group: "GroupMessage",
		baseline: true,
		fn: async () => {
			w.reset();
			await msg.encode(w);
		},
	});

	const enc = new BufWriter(64);
	await msg.encode(enc);
	const encoded = enc.bytes.slice();
	const r = new BufReader(encoded);
	const dst = new GroupMessage();
	Deno.bench({
		name: "GroupMessage.decode",
		group: "GroupMessage",
		fn: async () => {
			r.rewind();
			await dst.decode(r);
		},
	});
}

// SubscribeMessage: string-heavy control message.
{
	const init = {
		subscribeId: 42,
		broadcastPath: SHORT_PATH,
		trackName: "camera-hd",
		subscriberPriority: 5,
		subscriberOrdered: 1,
		subscriberMaxLatency: 100,
		startGroup: 0,
		endGroup: 1000,
	};
	const w = new BufWriter(128);
	const msg = new SubscribeMessage(init);
	Deno.bench({
		name: "SubscribeMessage.encode",
		group: "SubscribeMessage",
		baseline: true,
		fn: async () => {
			w.reset();
			await msg.encode(w);
		},
	});

	const enc = new BufWriter(128);
	await msg.encode(enc);
	const encoded = enc.bytes.slice();
	const r = new BufReader(encoded);
	const dst = new SubscribeMessage();
	Deno.bench({
		name: "SubscribeMessage.decode",
		group: "SubscribeMessage",
		fn: async () => {
			r.rewind();
			await dst.decode(r);
		},
	});
}
