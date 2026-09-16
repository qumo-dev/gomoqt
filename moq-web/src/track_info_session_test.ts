import { assertEquals, assertExists } from "@std/assert";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import { TrackInfoMessage } from "./internal/message/mod.ts";
import { BiStreamTypes } from "./stream_type.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

async function trackInfoBytes(timescale: number): Promise<Uint8Array> {
	const buf = Buffer.make(128);
	await new TrackInfoMessage({
		publisherPriority: 5,
		publisherOrdered: 1,
		publisherMaxLatency: 2000,
		timescale,
	}).encode(buf);
	return buf.bytes();
}

Deno.test("Session.trackInfo returns parsed publisher properties", async () => {
	const rsp = await trackInfoBytes(90000);
	const mock = new MockWebTransportSession({ openStreamResponses: [rsp] });
	const session = new Session({ transport: mock });
	await session.ready;

	const [info, err] = await session.trackInfo("/live/alice", "video");
	assertEquals(err, undefined);
	assertExists(info);
	assertEquals(info!.priority, 5);
	assertEquals(info!.ordered, true);
	assertEquals(info!.maxLatency, 2000);
	assertEquals(info!.timescale, 90000);

	// The request must be a Track stream: first written byte is TrackStreamType.
	const written = mock.openStreamWrittenData[0] ?? [];
	const first = (written[0] ?? new Uint8Array())[0];
	assertEquals(first, BiStreamTypes.TrackStreamType);

	await session.close();
});

Deno.test("Session.trackInfo rejects a zero Timescale", async () => {
	const rsp = await trackInfoBytes(0);
	const mock = new MockWebTransportSession({ openStreamResponses: [rsp] });
	const session = new Session({ transport: mock });
	await session.ready;

	const [info, err] = await session.trackInfo("/p", "t");
	assertEquals(info, undefined);
	assertEquals(err instanceof Error, true);

	await session.close();
});
