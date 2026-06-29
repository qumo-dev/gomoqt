/**
 * Frame-size sweep for the GroupWriter.writeFrame data path.
 *
 * Compares the current two-write shape (varint length prefix, then the payload)
 * against a single-write "combined" shape ([varint ++ payload] in one buffer),
 * across realistic frame sizes, for a raw Uint8Array payload (the common case —
 * see msf/broadcast.ts and typical media producers).
 *
 * The mock SendStream's `write` COPIES each chunk into a reusable sink buffer,
 * modelling the fact that WebTransport copies every chunk into the QUIC send
 * buffer natively. Without that, a benchmark unfairly favours "combined": it
 * hides that combining makes JS copy the payload that native would copy anyway,
 * i.e. the payload is copied twice (JS concat + native) instead of once.
 *
 * Run: deno bench --allow-all src/group_stream_writeframe_benchmark.ts
 */

import type { Writer } from "@okdaichi/golikejs/io";
import { putVarint, varintLen, writeVarint } from "./internal/message/mod.ts";

/** Mock writer that copies each chunk into a reusable buffer (≈ native QUIC send-buffer copy). */
function copyingWriter(): Writer {
	const sink = new Uint8Array(512 * 1024);
	return {
		// deno-lint-ignore require-await
		async write(p: Uint8Array): Promise<[number, Error | undefined]> {
			sink.set(p.subarray(0, Math.min(p.length, sink.length)));
			return [p.length, undefined];
		},
	};
}

/** Current shape: write the varint length, then the payload (two writes, zero-copy payload). */
async function writeFrameTwoWrites(w: Writer, bytes: Uint8Array): Promise<void> {
	await writeVarint(w, bytes.byteLength);
	await w.write(bytes);
}

/** Combined shape: one buffer holding [varint ++ payload], single write (one JS copy of the payload). */
async function writeFrameCombined(w: Writer, bytes: Uint8Array): Promise<void> {
	const prefix = varintLen(bytes.byteLength);
	const out = new Uint8Array(prefix + bytes.byteLength);
	putVarint(out, 0, bytes.byteLength);
	out.set(bytes, prefix);
	await w.write(out);
}

const SIZES: Record<string, number> = {
	"256 B (audio)": 256,
	"4 KB": 4 * 1024,
	"64 KB": 64 * 1024,
	"256 KB (keyframe)": 256 * 1024,
};

for (const [label, size] of Object.entries(SIZES)) {
	const w = copyingWriter();
	const bytes = new Uint8Array(size);

	Deno.bench({
		name: `writeFrame current (2 writes) — ${label}`,
		group: label,
		baseline: true,
		fn: async () => {
			await writeFrameTwoWrites(w, bytes);
		},
	});

	Deno.bench({
		name: `writeFrame combined (1 write) — ${label}`,
		group: label,
		fn: async () => {
			await writeFrameCombined(w, bytes);
		},
	});
}
