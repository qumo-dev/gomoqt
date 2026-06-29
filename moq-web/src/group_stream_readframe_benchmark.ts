/**
 * Frame-size sweep for the GroupReader.readFrame receive path (Frame sink — the
 * `frames()` loop's case).
 *
 * Current shape allocates a fresh `new Uint8Array(len)` per frame, reads into
 * it, then `sink.write(buf)` copies it again into the Frame's internal buffer —
 * a per-frame temp allocation plus a redundant full-payload copy.
 *
 * Optimized shape reads straight into the Frame's (reused) internal buffer: no
 * temp allocation, no second copy.
 *
 * The mock reader copies `len` bytes into the caller's buffer on each read
 * (the unavoidable stream->target copy both variants pay), so the measured
 * delta is exactly the temp-alloc + redundant copy the optimization removes.
 *
 * Run: deno bench --allow-all src/group_stream_readframe_benchmark.ts
 */

import type { Reader } from "@okdaichi/golikejs/io";
import { readFull } from "./internal/message/mod.ts";

/** Mock reader that fills the caller's buffer (≈ stream->target copy on read). */
function fillingReader(size: number): Reader {
	const source = new Uint8Array(size);
	let pos = 0;
	return {
		// deno-lint-ignore require-await
		async read(p: Uint8Array): Promise<[number, Error | undefined]> {
			const n = Math.min(p.length, size - pos);
			p.set(source.subarray(pos, pos + n));
			pos += n;
			if (pos >= size) pos = 0; // loop for the next bench iteration
			return [n, undefined];
		},
	};
}

/** Current: temp Uint8Array(len) -> readFull -> copy into the Frame buffer (sink.write). */
async function readFrameCurrent(
	r: Reader,
	frameBuf: { buf: ArrayBuffer; len: number },
	len: number,
) {
	const tmp = new Uint8Array(len);
	await readFull(r, tmp);
	// BytesBuffer.write: grow if needed, then copy.
	if (frameBuf.buf.byteLength < len) frameBuf.buf = new ArrayBuffer(len);
	new Uint8Array(frameBuf.buf, 0, len).set(tmp);
	frameBuf.len = len;
}

/** Optimized: reserve the Frame buffer and readFull straight into it (no temp, no second copy). */
async function readFrameOptimized(
	r: Reader,
	frameBuf: { buf: ArrayBuffer; len: number },
	len: number,
) {
	if (frameBuf.buf.byteLength < len) frameBuf.buf = new ArrayBuffer(len);
	const dst = new Uint8Array(frameBuf.buf, 0, len);
	await readFull(r, dst);
	frameBuf.len = len;
}

const SIZES: Record<string, number> = {
	"256 B (audio)": 256,
	"4 KB": 4 * 1024,
	"64 KB": 64 * 1024,
	"256 KB (keyframe)": 256 * 1024,
};

for (const [label, size] of Object.entries(SIZES)) {
	{
		const r = fillingReader(size);
		const frame = { buf: new ArrayBuffer(size), len: 0 };
		Deno.bench({
			name: `readFrame current (temp + copy) — ${label}`,
			group: label,
			baseline: true,
			fn: async () => {
				await readFrameCurrent(r, frame, size);
			},
		});
	}
	{
		const r = fillingReader(size);
		const frame = { buf: new ArrayBuffer(size), len: 0 };
		Deno.bench({
			name: `readFrame optimized (read into Frame) — ${label}`,
			group: label,
			fn: async () => {
				await readFrameOptimized(r, frame, size);
			},
		});
	}
}
