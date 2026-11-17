import { assertEquals, assertInstanceOf } from "@std/assert";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { GroupOutOfRangeError, TrackWriter } from "./track_writer.ts";
import { ReceiveSubscribeStream, validateTrackConfig } from "./subscribe_stream.ts";
import type { SendStream } from "./internal/webtransport/mod.ts";
import { MockSendStream, MockStream } from "./mock_stream_test.ts";
import {
	AnnounceEndMessage,
	AnnounceStartMessage,
	AnnounceUpdateMessage,
	DEFAULT_COST,
	FetchMessage,
	GoawayMessage,
	GroupMessage,
	MAX_NEW_SESSION_URI_LEN,
	NewSessionURITooLongError,
	readVarint,
	SetupMessage,
	SubscribeEndMessage,
	SubscribeMessage,
	SubscribeUpdateMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import { GroupAges } from "./group_ages.ts";
import { FetchRequest } from "./fetch.ts";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import { BiStreamTypes } from "./stream_type.ts";
import { SessionErrorCode } from "./error.ts";

/** A send stream that keeps everything written to it. */
class FakeRecordingSendStream {
	readonly buf = Buffer.make(1024);
	readonly stream = new MockSendStream({ write: (p) => this.buf.write(p) });
}

/** A ReceiveSubscribeStream whose writes are recorded. */
function subscription(init: ConstructorParameters<typeof SubscribeMessage>[0]): {
	rss: ReceiveSubscribeStream;
	control: FakeRecordingSendStream;
} {
	const [ctx] = withCancelCause(background());
	const control = new FakeRecordingSendStream();
	const rss = new ReceiveSubscribeStream(
		ctx,
		new MockStream({ writable: control.stream }),
		new SubscribeMessage({ subscribeId: 1, broadcastPath: "/b", trackName: "t", ...init }),
	);
	return { rss, control };
}

/** A TrackWriter whose group streams are recorded, newest last. */
function trackWriter(rss: ReceiveSubscribeStream): {
	tw: TrackWriter;
	groups: FakeRecordingSendStream[];
} {
	const groups: FakeRecordingSendStream[] = [];
	const openUni = () => {
		const g = new FakeRecordingSendStream();
		groups.push(g);
		return Promise.resolve([g.stream, undefined] as [SendStream, undefined]);
	};
	return { tw: new TrackWriter("/b", "t", rss, openUni), groups };
}

/** Reads a group stream's header and returns its GROUP message and frame count. */
async function readGroup(g: FakeRecordingSendStream): Promise<[GroupMessage, number]> {
	await readVarint(g.buf); // stream type
	const msg = new GroupMessage();
	await msg.decode(g.buf);
	let frames = 0;
	while (g.buf.len() > 0) {
		await readVarint(g.buf); // timestamp delta
		const [len] = await readVarint(g.buf);
		g.buf.read(new Uint8Array(len));
		frames++;
	}
	return [msg, frames];
}

Deno.test("draft-06 wire: new fields round-trip", async (t) => {
	const cases = [
		{
			name: "SUBSCRIBE with Max Age and frame bounds",
			msg: new SubscribeMessage({
				subscribeId: 2,
				broadcastPath: "/live",
				trackName: "v",
				subscriberMaxAge: 1500,
				groupStart: 5,
				groupEnd: 11,
				frameStart: 15,
				frameEnd: 31,
			}),
			fresh: () => new SubscribeMessage(),
		},
		{
			name: "SUBSCRIBE_UPDATE with frame bounds",
			msg: new SubscribeUpdateMessage({ subscriberMaxAge: 9, frameStart: 4, frameEnd: 8 }),
			fresh: () => new SubscribeUpdateMessage(),
		},
		{
			name: "FETCH with a frame range",
			msg: new FetchMessage({
				broadcastPath: "/a",
				groupSequence: 42,
				frameStart: 2,
				frameEnd: 7,
			}),
			fresh: () => new FetchMessage(),
		},
		{
			name: "GROUP with Frame Start",
			msg: new GroupMessage({ subscribeId: 1, sequence: 7, frameStart: 15 }),
			fresh: () => new GroupMessage(),
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const buf = Buffer.make(256);
			await c.msg.encode(buf);
			const decoded = c.fresh();

			const err = await decoded.decode(buf);

			assertEquals(err, undefined);
			assertEquals(decoded, c.msg);
		});
	}

	await t.step("announcements carry their Type tag before the length", async () => {
		const msgs = [
			[
				0x0,
				new AnnounceStartMessage({ suffix: "a", hopIDs: [7, 0], warmCost: 3, coldCost: 4 }),
			],
			[0x1, new AnnounceEndMessage({ announceId: 300 })],
			[
				0x2,
				new AnnounceUpdateMessage({ announceId: 2, hopIDs: [9], warmCost: 1, coldCost: 2 }),
			],
		] as const;
		for (const [type, msg] of msgs) {
			const buf = Buffer.make(64);
			await msg.encode(buf);
			const [got] = await readVarint(buf);
			const decoded = new (msg.constructor as new () => typeof msg)();

			const err = await decoded.decode(buf);

			assertEquals(got, type);
			assertEquals(err, undefined);
			assertEquals(decoded, msg);
		}
	});

	await t.step("a Hop Count larger than its entries is malformed", async () => {
		const buf = Buffer.make(16);
		// length 4: suffix "", Hop Count 3, one Hop ID, warm cost (no cold cost)
		await buf.write(new Uint8Array([0x04, 0x00, 0x03, 0x05, 0x00]));

		const err = await new AnnounceStartMessage().decode(buf);

		assertInstanceOf(err, Error);
	});
});

Deno.test("draft-06 GOAWAY URI limit", async (t) => {
	await t.step("encode refuses an over-long URI", async () => {
		const gm = new GoawayMessage({ newSessionURI: "a".repeat(MAX_NEW_SESSION_URI_LEN + 1) });

		const err = await gm.encode(Buffer.make(16));

		assertInstanceOf(err, NewSessionURITooLongError);
	});

	await t.step("a URI at the limit round-trips", async () => {
		const gm = new GoawayMessage({ newSessionURI: "a".repeat(MAX_NEW_SESSION_URI_LEN) });
		const buf = Buffer.make(MAX_NEW_SESSION_URI_LEN + 16);
		await gm.encode(buf);
		const decoded = new GoawayMessage();

		const err = await decoded.decode(buf);

		assertEquals(err, undefined);
		assertEquals(decoded.newSessionURI.length, MAX_NEW_SESSION_URI_LEN);
	});

	await t.step("decode refuses an over-long URI from its length", async () => {
		const buf = Buffer.make(16);
		await writeVarint(buf, MAX_NEW_SESSION_URI_LEN + 100);

		const err = await new GoawayMessage().decode(buf);

		assertInstanceOf(err, NewSessionURITooLongError);
	});
});

Deno.test("draft-06 SETUP Cost and Hop", async (t) => {
	await t.step("absent parameters mean the default cost and no hop", () => {
		const sm = new SetupMessage();

		assertEquals([sm.cost(), sm.hopId()], [DEFAULT_COST, 0]);
	});

	await t.step("declared values round-trip, a zero cost included", async () => {
		const sm = new SetupMessage();
		sm.addCost(0);
		sm.addHop(77);
		const buf = Buffer.make(64);
		await sm.encode(buf);
		const decoded = new SetupMessage();
		await decoded.decode(buf);

		assertEquals([decoded.cost(), decoded.hopId()], [0, 77]);
	});
});

Deno.test("draft-06 range rules", async (t) => {
	await t.step("endFrame without endGroup is refused before sending", () => {
		const err = validateTrackConfig({
			priority: 0,
			maxAge: 0,
			startGroup: 0,
			endGroup: 0,
			endFrame: 3,
		});

		assertInstanceOf(err, Error);
	});

	await t.step("a fetch frameEnd not above frameStart is refused", () => {
		const req = new FetchRequest({
			broadcastPath: "/a",
			trackName: "t",
			priority: 0,
			groupSequence: 1,
			frameStart: 4,
			frameEnd: 4,
		});

		assertInstanceOf(req.validate(), Error);
	});
});

Deno.test("draft-06 TrackWriter", async (t) => {
	await t.step("a group below the Group Start floor is refused", async () => {
		const { rss } = subscription({ groupStart: 5 });
		const { tw } = trackWriter(rss);

		const [, err] = await tw.openGroupAt(4);

		assertInstanceOf(err, GroupOutOfRangeError);
	});

	await t.step("a group above Group End is refused", async () => {
		const { rss } = subscription({ groupEnd: 3 + 1 });
		const { tw } = trackWriter(rss);

		const [, err] = await tw.openGroupAt(4);

		assertInstanceOf(err, GroupOutOfRangeError);
	});

	await t.step("the start group skips frames before Frame Start", async () => {
		const { rss } = subscription({ groupStart: 2, frameStart: 2 });
		const { tw, groups } = trackWriter(rss);
		const [gw] = await tw.openGroupAt(2);

		for (let i = 0; i < 5; i++) await gw!.writeFrame(new Uint8Array([i]), i);

		const [msg, frames] = await readGroup(groups[0]!);
		assertEquals([msg.frameStart, frames], [2, 3]);
	});

	await t.step("the end group stops before Frame End", async () => {
		const { rss } = subscription({ groupEnd: 3 + 1, frameEnd: 2 });
		const { tw, groups } = trackWriter(rss);
		const [gw] = await tw.openGroupAt(3);

		for (let i = 0; i < 5; i++) await gw!.writeFrame(new Uint8Array([i]), i);

		const [msg, frames] = await readGroup(groups[0]!);
		assertEquals([msg.frameStart, frames], [0, 2]);
	});

	const endCases = [
		{ name: "no group used sends 0", open: [] as number[], want: 0 },
		{ name: "groups 1 and 2 send 3", open: [1, 2], want: 3 },
		{ name: "only group 0 sends 1", open: [0], want: 1 },
	];
	for (const c of endCases) {
		await t.step(`SUBSCRIBE_END is exclusive: ${c.name}`, async () => {
			const { rss, control } = subscription({});
			const { tw } = trackWriter(rss);
			for (const seq of c.open) await tw.openGroupAt(seq);

			await tw.close();

			// Skip SUBSCRIBE_OK when a group opened it.
			let type = (await readVarint(control.buf))[0];
			if (type === 0x0) {
				await new SubscribeEndMessage().decode(control.buf); // SUBSCRIBE_OK has the same shape
				type = (await readVarint(control.buf))[0];
			}
			const end = new SubscribeEndMessage();
			await end.decode(control.buf);
			assertEquals([type, end.group], [0x1, c.want]);
		});
	}
});

/** A group the tracker can expire, recording whether it was. */
class FakeAgedGroup {
	expired = false;
	constructor(readonly sequence: number) {}
	expire(): void {
		this.expired = true;
	}
}

Deno.test("draft-06 Max Age expiry", async (t) => {
	await t.step("a group whose successor has no frame never expires", () => {
		const ages = new GroupAges(() => 0, 1000);
		const g1 = new FakeAgedGroup(1);
		ages.add(g1);
		ages.add(new FakeAgedGroup(2));

		ages.observe(g1, 100);

		assertEquals(g1.expired, false);
	});

	await t.step("an age equal to Max Age expires the group", () => {
		const ages = new GroupAges(() => 100, 1000);
		const g1 = new FakeAgedGroup(1);
		const g2 = new FakeAgedGroup(2);
		ages.add(g1);
		ages.add(g2);
		ages.observe(g1, 0);
		ages.observe(g2, 500); // g1's reach

		ages.observe(g2, 600); // age 100

		assertEquals(g1.expired, true);
	});

	await t.step("an age below Max Age keeps the group", () => {
		const ages = new GroupAges(() => 100, 1000);
		const g1 = new FakeAgedGroup(1);
		const g2 = new FakeAgedGroup(2);
		ages.add(g1);
		ages.add(g2);
		ages.observe(g1, 0);
		ages.observe(g2, 500);

		ages.observe(g2, 599);

		assertEquals(g1.expired, false);
	});

	await t.step("a Max Age of 0 keeps only the latest group", () => {
		const ages = new GroupAges(() => 0, 1000);
		const g1 = new FakeAgedGroup(1);
		const g2 = new FakeAgedGroup(2);
		ages.add(g1);
		ages.add(g2);
		ages.observe(g1, 0);

		ages.observe(g2, 10);

		assertEquals([g1.expired, g2.expired], [true, false]);
	});
});

Deno.test("draft-06 GOAWAY: a second GOAWAY is a protocol violation", async () => {
	const goaway = async () => {
		const buf = Buffer.make(32);
		await writeVarint(buf, BiStreamTypes.GoawayStreamType);
		await new GoawayMessage().encode(buf);
		return { type: BiStreamTypes.GoawayStreamType, data: buf.bytes() };
	};
	const mock = new MockWebTransportSession({
		acceptStreamData: [await goaway(), await goaway()],
	});
	const session = new Session({ transport: mock });
	await session.ready;

	const info = await session.closed;

	assertEquals(info.code, SessionErrorCode.ProtocolViolation);
});
