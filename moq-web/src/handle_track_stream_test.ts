import { assertEquals } from "@std/assert";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import { Broadcast } from "./broadcast.ts";
import { TrackInfoMessage, TrackMessage, writeVarint } from "./internal/message/mod.ts";
import { BiStreamTypes } from "./stream_type.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { background } from "@okdaichi/golikejs/context";

async function trackRequestBytes(path: string, name: string): Promise<Uint8Array> {
	const buf = Buffer.make(64);
	await writeVarint(buf, BiStreamTypes.TrackStreamType);
	await new TrackMessage({ broadcastPath: path, trackName: name }).encode(buf);
	return buf.bytes();
}

Deno.test("Session handles incoming Track stream and replies with TRACK_INFO", async () => {
	const mux = new (await import("./track_mux.ts")).TrackMux();
	const b = new Broadcast();
	const info = { priority: 4, ordered: true, maxLatency: 300, timescale: 48000 };
	await b.registerWithInfo("audio", info, { serveTrack: () => {} });
	await mux.publish(background().done(), "/live", b);

	const data = await trackRequestBytes("/live", "audio");
	const mock = new MockWebTransportSession({
		acceptStreamData: [{ type: BiStreamTypes.TrackStreamType, data }],
	});
	const session = new Session({ transport: mock, mux });
	await session.ready;

	// The handler runs async on the bi-stream listener; wait for the write.
	const written = await waitForWrite(mock, 0);
	const tim = new TrackInfoMessage({});
	await tim.decode(toReader(written));
	assertEquals(tim.publisherPriority, 4);
	assertEquals(tim.publisherOrdered, 1);
	assertEquals(tim.publisherMaxLatency, 300);
	assertEquals(tim.timescale, 48000);

	await session.close();
});

Deno.test("Session Track stream for unknown track resets the stream", async () => {
	const mux = new (await import("./track_mux.ts")).TrackMux();
	const data = await trackRequestBytes("/missing", "audio");
	const mock = new MockWebTransportSession({
		acceptStreamData: [{ type: BiStreamTypes.TrackStreamType, data }],
	});
	const session = new Session({ transport: mock, mux });
	await session.ready;

	// No TRACK_INFO is written for an unknown track; nothing is captured.
	await new Promise((r) => setTimeout(r, 20));
	assertEquals((mock.acceptStreamWrittenData[0] ?? []).length, 0);

	await session.close();
});

// Poll until the n-th accepted stream has captured written bytes, then concat.
async function waitForWrite(
	mock: MockWebTransportSession,
	idx: number,
): Promise<Uint8Array> {
	for (let i = 0; i < 100; i++) {
		const chunks = mock.acceptStreamWrittenData[idx];
		if (chunks && chunks.length > 0) {
			const total = chunks.reduce((n, c) => n + c.length, 0);
			const out = new Uint8Array(total);
			let off = 0;
			for (const c of chunks) {
				out.set(c, off);
				off += c.length;
			}
			return out;
		}
		await new Promise((r) => setTimeout(r, 5));
	}
	throw new Error("timeout waiting for handler write");
}

function toReader(data: Uint8Array) {
	let off = 0;
	return {
		read: async (p: Uint8Array): Promise<[number, Error | undefined]> => {
			if (off >= data.length) return [0, new Error("EOF")];
			const n = Math.min(p.length, data.length - off);
			p.set(data.subarray(off, off + n));
			off += n;
			return [n, undefined];
		},
	};
}
