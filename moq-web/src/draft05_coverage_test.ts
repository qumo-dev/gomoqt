import { assertEquals } from "@std/assert";
import { background } from "@okdaichi/golikejs/context";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { Broadcast } from "./broadcast.ts";
import { TrackMux } from "./track_mux.ts";
import { MockReceiveStream, MockSendStream, MockStream } from "./mock_stream_test.ts";
import {
	readSubscribeResponse,
	ReceiveSubscribeStream,
	SendSubscribeStream,
} from "./subscribe_stream.ts";
import {
	MESSAGE_TYPE_SUBSCRIBE_DROP,
	MESSAGE_TYPE_SUBSCRIBE_END,
	MESSAGE_TYPE_SUBSCRIBE_OK,
} from "./subscribe_stream.ts";
import {
	SubscribeDropMessage,
	SubscribeEndMessage,
	SubscribeMessage,
	SubscribeOkMessage,
} from "./internal/message/mod.ts";

const nopHandler = { serveTrack: () => {} };

// A Reader over a fixed byte buffer with its own offset.
class BufReader {
	#data: Uint8Array;
	#off = 0;
	constructor(data: Uint8Array) {
		this.#data = data;
	}
	async read(p: Uint8Array): Promise<[number, Error | undefined]> {
		if (this.#off >= this.#data.length) return [0, new Error("EOF")];
		const n = Math.min(p.length, this.#data.length - this.#off);
		p.set(this.#data.subarray(this.#off, this.#off + n));
		this.#off += n;
		return [n, undefined];
	}
}

// A Writer that appends into a golikejs Buffer.
class BufWriter {
	#buf: ReturnType<typeof Buffer.make>;
	constructor(buf: ReturnType<typeof Buffer.make>) {
		this.#buf = buf;
	}
	async write(p: Uint8Array): Promise<[number, Error | undefined]> {
		this.#buf.write(p);
		return [p.length, undefined];
	}
}

// ---- TrackMux.trackInfo + Broadcast.registerWithInfo/trackInfo ----

Deno.test("Broadcast.registerWithInfo/trackInfo", async () => {
	const b = new Broadcast();
	const info = { priority: 7, ordered: true, maxLatency: 500, timescale: 48000 };
	await b.registerWithInfo("audio", info, nopHandler);
	await b.register("video", nopHandler);

	assertEquals(b.trackInfo("audio"), info);
	assertEquals(b.trackInfo("video"), {
		priority: 0,
		ordered: false,
		maxLatency: 0,
		timescale: 0,
	});
	assertEquals(b.trackInfo("missing"), undefined);
});

Deno.test("TrackMux.trackInfo delegates to a Broadcast provider", async () => {
	const mux = new TrackMux();
	const b = new Broadcast();
	const info = { priority: 3, ordered: false, maxLatency: 0, timescale: 90000 };
	await b.registerWithInfo("video", info, nopHandler);
	await mux.publish(background().done(), "/live", b);

	assertEquals(mux.trackInfo("/live", "video"), info);
	assertEquals(mux.trackInfo("/live", "missing"), undefined);
	assertEquals(mux.trackInfo("/unknown", "video"), undefined);
});

Deno.test("TrackMux.trackInfo serves defaults for a plain handler", async () => {
	const mux = new TrackMux();
	await mux.publish(background().done(), "/plain", nopHandler);
	assertEquals(mux.trackInfo("/plain", "anything"), {
		priority: 0,
		ordered: false,
		maxLatency: 0,
		timescale: 1000,
	});
});

// ---- readSubscribeResponse: all publisher response types ----

async function framedReader(msgType: number, body: Uint8Array): Promise<BufReader> {
	const buf = Buffer.make(64);
	buf.write(new Uint8Array([msgType]));
	buf.write(body);
	return new BufReader(buf.bytes());
}

Deno.test("readSubscribeResponse decodes ok/end/drop and rejects unknown", async () => {
	const okBody = Buffer.make(16);
	await new SubscribeOkMessage({ group: 5 }).encode(new BufWriter(okBody));
	const r1 = await readSubscribeResponse(
		await framedReader(MESSAGE_TYPE_SUBSCRIBE_OK, okBody.bytes()),
	);
	assertEquals(r1[1], undefined);
	assertEquals(r1[0]?.ok?.group, 5);

	const endBody = Buffer.make(16);
	await new SubscribeEndMessage({ group: 9 }).encode(new BufWriter(endBody));
	const r2 = await readSubscribeResponse(
		await framedReader(MESSAGE_TYPE_SUBSCRIBE_END, endBody.bytes()),
	);
	assertEquals(r2[1], undefined);
	assertEquals(r2[0]?.end?.group, 9);

	const dropBody = Buffer.make(16);
	await new SubscribeDropMessage({ groupStart: 2, groupEnd: 4, errorCode: 3 }).encode(
		new BufWriter(dropBody),
	);
	const r3 = await readSubscribeResponse(
		await framedReader(MESSAGE_TYPE_SUBSCRIBE_DROP, dropBody.bytes()),
	);
	assertEquals(r3[1], undefined);
	assertEquals(r3[0]?.drop?.groupStart, 2);

	const r4 = await readSubscribeResponse(new BufReader(new Uint8Array([0x09])));
	assertEquals(r4[1] instanceof Error, true);
});

// ---- ReceiveSubscribeStream: writeOk, writeEnd (idempotent), writeDrop after ok ----

function newReceiveStream() {
	const buf = Buffer.make(128);
	const writable = new MockSendStream({ write: (p) => buf.write(p) });
	const stream = new MockStream({
		writable,
		readable: new MockReceiveStream({}),
	});
	const sub = new SubscribeMessage({
		subscribeId: 1,
		broadcastPath: "/p",
		trackName: "t",
		subscriberPriority: 0,
	});
	return { buf, rss: new ReceiveSubscribeStream(background(), stream, sub) };
}

Deno.test("ReceiveSubscribeStream.writeEnd is idempotent", async () => {
	const { buf, rss } = newReceiveStream();
	assertEquals(await rss.writeEnd(7), undefined);
	const firstLen = buf.bytes().length;
	assertEquals(await rss.writeEnd(8), undefined);
	assertEquals(buf.bytes().length, firstLen);
});

Deno.test("ReceiveSubscribeStream.writeDrop after writeOk emits explicit DROP", async () => {
	const { buf, rss } = newReceiveStream();
	assertEquals(await rss.writeOk(3), undefined);
	assertEquals(
		await rss.writeDrop({ startGroup: 4, endGroup: 6, errorCode: 2 }),
		undefined,
	);
	const b = buf.bytes();
	assertEquals(b[0], MESSAGE_TYPE_SUBSCRIBE_OK);
	// OK body = 1 msglen + 1 group varint = 2 bytes; DROP type at idx 3.
	assertEquals(b[3], MESSAGE_TYPE_SUBSCRIBE_DROP);
});

// ---- SendSubscribeStream.readSubscribeResponses: END + DROP state ----

Deno.test("SendSubscribeStream.readSubscribeResponses records end and drops", async () => {
	const buf = Buffer.make(128);
	const w = new BufWriter(buf);
	await writeFramed(
		w,
		MESSAGE_TYPE_SUBSCRIBE_OK,
		async (x) => await new SubscribeOkMessage({ group: 5 }).encode(x),
	);
	await writeFramed(
		w,
		MESSAGE_TYPE_SUBSCRIBE_END,
		async (x) => await new SubscribeEndMessage({ group: 11 }).encode(x),
	);
	await writeFramed(
		w,
		MESSAGE_TYPE_SUBSCRIBE_DROP,
		async (x) =>
			await new SubscribeDropMessage({ groupStart: 2, groupEnd: 3, errorCode: 4 }).encode(x),
	);

	const stream = new MockStream({
		readable: new MockReceiveStream({ read: (p) => readInto(buf, p) }),
	});
	const sub = new SubscribeMessage({
		subscribeId: 1,
		broadcastPath: "/p",
		trackName: "t",
		subscriberPriority: 0,
	});
	const sss = new SendSubscribeStream(background(), stream, sub);
	await sss.readSubscribeResponses();

	assertEquals(sss.resolvedStart, 5);
	assertEquals(sss.ended, true);
	assertEquals(sss.endGroup, 11);
	const drops = sss.pendingDrops();
	assertEquals(drops.length, 1);
	assertEquals(drops[0], { startGroup: 2, endGroup: 3, errorCode: 4 });
});

// readInto copies from buf (with a stable per-buffer offset stashed on buf).
function readInto(
	buf: ReturnType<typeof Buffer.make>,
	p: Uint8Array,
): Promise<[number, Error | undefined]> {
	const data = buf.bytes();
	const off = readIntoOffsets.get(buf) ?? 0;
	if (off >= data.length) return Promise.resolve([0, new Error("EOF")]);
	const n = Math.min(p.length, data.length - off);
	p.set(data.subarray(off, off + n));
	readIntoOffsets.set(buf, off + n);
	return Promise.resolve([n, undefined]);
}
const readIntoOffsets = new WeakMap<ReturnType<typeof Buffer.make>, number>();

async function writeFramed(
	w: BufWriter,
	msgType: number,
	encode: (
		w: { write: (p: Uint8Array) => Promise<[number, Error | undefined]> },
	) => Promise<unknown>,
) {
	await w.write(new Uint8Array([msgType]));
	await encode(w);
}
