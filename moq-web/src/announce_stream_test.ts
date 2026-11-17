import { assertEquals, assertExists, assertInstanceOf, assertStrictEquals } from "@std/assert";
import {
	Announcement,
	AnnouncementReader,
	AnnouncementWriter,
	routeSuffix,
} from "./announce_stream.ts";
import { background, withCancelCause } from "@okdaichi/golikejs/context";
import {
	AnnounceEndMessage,
	AnnounceMessageTypes,
	AnnounceOkMessage,
	AnnounceRequestMessage,
	AnnounceStartMessage,
	AnnounceUpdateMessage,
	readVarint,
	writeVarint,
} from "./internal/message/mod.ts";
import type { AnnounceMessage } from "./internal/message/mod.ts";
import { MockReceiveStream, MockSendStream, MockStream } from "./mock_stream_test.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { EOFError } from "@okdaichi/golikejs/io";
import { AnnounceError, AnnounceErrorCode } from "./error.ts";
import { WebTransportStreamError } from "./internal/webtransport/error.ts";

// openRead serves buf, then parks like a peer that keeps the stream open:
// a reader handed buf.read directly sees the end of the stream once buf is
// drained, which ends every announcement it holds.
function openRead(buf: Buffer): (p: Uint8Array) => Promise<[number, Error | undefined]> {
	return (p) => buf.len() > 0 ? buf.read(p) : new Promise(() => {});
}

/** Writes an announcement as the Announce Stream does: Type tag, then body. */
async function writeAnnouncement(buf: Buffer, msg: AnnounceMessage): Promise<void> {
	await writeVarint(buf, msg.type);
	await msg.encode(buf);
}

/** Records the session-closing violations a reader reports. */
class FakeViolations {
	readonly reasons: string[] = [];
	readonly report = (reason: string): void => {
		this.reasons.push(reason);
	};
}

/** Starts a reader over buf, ANNOUNCE_OK first, then the given messages. */
async function readerOver(
	prefix: string,
	messages: AnnounceMessage[],
	options: { okHop?: number; localHopId?: number; peerCost?: number } = {},
): Promise<{ reader: AnnouncementReader; violations: FakeViolations; done: () => void }> {
	const buf = Buffer.make(512);
	await new AnnounceOkMessage({ hopID: options.okHop ?? 0 }).encode(buf);
	for (const m of messages) await writeAnnouncement(buf, m);
	const violations = new FakeViolations();
	const [ctx, cancel] = withCancelCause(background());
	const reader = new AnnouncementReader(
		ctx,
		new MockStream({ readable: new MockReceiveStream({ read: openRead(buf) }) }),
		new AnnounceRequestMessage({ prefix }),
		{
			localHopId: options.localHopId,
			peerCost: options.peerCost === undefined
				? undefined
				: () => Promise.resolve(options.peerCost!),
			onProtocolViolation: violations.report,
		},
	);
	return { reader, violations, done: () => cancel(new Error("test cleanup")) };
}

/** Decodes every typed announcement after ANNOUNCE_OK in bytes. */
async function decodeAnnouncements(bytes: Uint8Array): Promise<
	(AnnounceStartMessage | AnnounceEndMessage | AnnounceUpdateMessage)[]
> {
	const buf = Buffer.make(bytes.length);
	await buf.write(bytes);
	const ok = new AnnounceOkMessage();
	await ok.decode(buf);
	const out: (AnnounceStartMessage | AnnounceEndMessage | AnnounceUpdateMessage)[] = [];
	while (buf.len() > 0) {
		const [type] = await readVarint(buf);
		const m = type === AnnounceMessageTypes.Start
			? new AnnounceStartMessage()
			: type === AnnounceMessageTypes.End
			? new AnnounceEndMessage()
			: new AnnounceUpdateMessage();
		await m.decode(buf);
		out.push(m);
	}
	return out;
}

const tick = () => new Promise((r) => setTimeout(r, 10));

Deno.test("Announcement", async (t) => {
	await t.step("lifecycle: isActive and ended", async () => {
		const [ctx] = withCancelCause(background());
		const ann = new Announcement("/some/path", ctx.done());
		assertEquals(ann.isActive(), true);
		const endedPromise = ann.ended();
		ann.end();
		await endedPromise;
		assertEquals(ann.isActive(), false);
	});

	await t.step("afterFunc executes registered function once", async () => {
		const [ctx, cancel] = withCancelCause(background());
		const ann = new Announcement("/some/path", ctx.done());
		let ran = false;
		const rv = ann.afterFunc(() => {
			ran = true;
		});
		ann.end();
		await ann.ended();
		assertEquals(ran, true);
		assertEquals(rv(), false);
		cancel(undefined);
	});
});

Deno.test("routeSuffix", () => {
	const cases = [
		{ name: "path under prefix", path: "/live/alice", prefix: "/live/", want: "alice" },
		{ name: "route above prefix clamps", path: "/live", prefix: "/live/alice/", want: "" },
		{ name: "root route clamps", path: "/", prefix: "/live/", want: "" },
		{ name: "route ending in / clamps", path: "/live/", prefix: "/live/alice/", want: "" },
		{
			name: "half a segment ending in /",
			path: "/li/",
			prefix: "/live/alice/",
			want: undefined,
		},
		{ name: "half a segment", path: "/li", prefix: "/live/", want: undefined },
		{ name: "unrelated path", path: "/other", prefix: "/live/", want: undefined },
	];
	for (const c of cases) {
		assertEquals(routeSuffix(c.path, c.prefix), c.want, c.name);
	}
});

Deno.test("AnnouncementWriter", async (t) => {
	const writerFor = (prefix: string, excludeHop = 0) => {
		const written = Buffer.make(1024);
		const [ctx, cancel] = withCancelCause(background());
		const writer = new AnnouncementWriter(
			ctx,
			new MockStream({ writable: new MockSendStream({ write: (p) => written.write(p) }) }),
			new AnnounceRequestMessage({ prefix }),
			excludeHop,
		);
		return { writer, written, ctx, cancel };
	};

	await t.step("init assigns Announce IDs and sends ANNOUNCE_START with zero costs", async () => {
		const { writer, written, ctx } = writerFor("/test/");

		const err = await writer.init([
			new Announcement("/test/a", ctx.done()),
			new Announcement("/test/b", ctx.done()),
		]);

		assertEquals(err, undefined);
		const msgs = await decodeAnnouncements(written.bytes());
		assertEquals(msgs.length, 2);
		for (const m of msgs) {
			assertInstanceOf(m, AnnounceStartMessage);
			assertEquals([m.warmCost, m.coldCost], [0, 0]);
		}
	});

	await t.step("an ended announcement is retracted by its Announce ID", async () => {
		const { writer, written, ctx } = writerFor("/p/");
		await writer.init([new Announcement("/p/a", ctx.done())]);
		const b = new Announcement("/p/b", ctx.done());
		await writer.send(b);

		b.end();
		await tick();

		const msgs = await decodeAnnouncements(written.bytes());
		const last = msgs[msgs.length - 1];
		assertInstanceOf(last, AnnounceEndMessage);
		assertEquals(last.announceId, 1);
	});

	await t.step(
		"a second announcement for an advertised route is sent as ANNOUNCE_UPDATE",
		async () => {
			const { writer, written, ctx } = writerFor("/p/");
			await writer.init([new Announcement("/p/a", ctx.done())]);

			const err = await writer.send(new Announcement("/p/a", ctx.done(), [5]));

			assertEquals(err, undefined);
			const msgs = await decodeAnnouncements(written.bytes());
			assertEquals(msgs.length, 2);
			const update = msgs[1];
			assertInstanceOf(update, AnnounceUpdateMessage);
			assertEquals(update.announceId, 0);
			assertEquals(update.hopIDs, [5]);
		},
	);

	await t.step("a route above the prefix is presented clamped, as an empty suffix", async () => {
		const { writer, written, ctx } = writerFor("/live/alice/");

		await writer.init([new Announcement("/live", ctx.done())]);

		const msgs = await decodeAnnouncements(written.bytes());
		assertInstanceOf(msgs[0], AnnounceStartMessage);
		assertEquals((msgs[0] as AnnounceStartMessage).suffix, "");
	});

	await t.step("a path through the peer's Hop ID is not advertised", async () => {
		const { writer, written, ctx } = writerFor("/p/", 9);

		await writer.init([new Announcement("/p/a", ctx.done(), [3, 9])]);

		assertEquals((await decodeAnnouncements(written.bytes())).length, 0);
	});

	await t.step("init returns an error for a path outside the prefix", async () => {
		const { writer, ctx } = writerFor("/test/");

		const err = await writer.init([new Announcement("/wrong/abc", ctx.done())]);

		assertInstanceOf(err, Error);
	});

	await t.step("send returns an error when ending a route not advertised", async () => {
		const { writer, ctx } = writerFor("/p/");
		await writer.init([]);
		const ann = new Announcement("/p/b", ctx.done());
		ann.end();

		const err = await writer.send(ann);

		assertInstanceOf(err, Error);
	});

	await t.step("send after a failed init returns init's error instead of waiting", async () => {
		const { writer, ctx } = writerFor("/test/");
		const initErr = await writer.init([new Announcement("/wrong/abc", ctx.done())]);

		const err = await writer.send(new Announcement("/test/a", ctx.done()));

		assertStrictEquals(err, initErr);
	});

	await t.step(
		"ending an announcement that shares a clamped route leaves the advertised one active",
		async () => {
			const { writer, ctx } = writerFor("/live/");
			const root = new Announcement("/", ctx.done());
			await writer.init([root]);
			const other = new Announcement("/live", ctx.done());
			other.end();

			const err = await writer.send(other);

			assertEquals(err, undefined);
			assertEquals(root.isActive(), true);
		},
	);

	await t.step("a route whose ANNOUNCE_START failed starts again on the next send", async () => {
		const written = Buffer.make(1024);
		let fail = false;
		const [ctx] = withCancelCause(background());
		const writer = new AnnouncementWriter(
			ctx,
			new MockStream({
				writable: new MockSendStream({
					write: (p) =>
						fail
							? Promise.resolve<[number, Error | undefined]>([
								0,
								new Error("write failed"),
							])
							: written.write(p),
				}),
			}),
			new AnnounceRequestMessage({ prefix: "/p/" }),
			0,
		);
		await writer.init([]);
		fail = true;
		await writer.send(new Announcement("/p/a", ctx.done()));
		fail = false;

		const err = await writer.send(new Announcement("/p/a", ctx.done()));

		assertEquals(err, undefined);
		const msgs = await decodeAnnouncements(written.bytes());
		assertInstanceOf(msgs[msgs.length - 1], AnnounceStartMessage);
	});

	await t.step("close does nothing when context already has error", async () => {
		const [ctx, cancelFunc] = withCancelCause(background());
		cancelFunc(new Error("already canceled"));
		await new Promise((r) => setTimeout(r, 0));
		let closes = 0;
		const writer = new AnnouncementWriter(
			ctx,
			new MockStream({
				writable: new MockSendStream({
					close: async () => {
						closes++;
					},
				}),
			}),
			new AnnounceRequestMessage({ prefix: "/test/" }),
		);

		await writer.close();

		assertEquals(closes, 0);
	});
});

Deno.test("AnnouncementReader", async (t) => {
	await t.step("receives an announcement from ANNOUNCE_START", async () => {
		const { reader, done } = await readerOver("/x/", [
			new AnnounceStartMessage({ suffix: "a" }),
		]);

		const [ann, err] = await reader.receive(new Promise(() => {}));

		assertEquals(err, undefined);
		assertEquals(ann?.broadcastPath, "/x/a");
		assertEquals(ann?.isActive(), true);
		done();
	});

	await t.step("reconstructs the hop path with the ANNOUNCE_OK hop, even 0", async () => {
		const { reader, done } = await readerOver("/", [
			new AnnounceStartMessage({ suffix: "a", hopIDs: [7] }),
		], { okHop: 0 });

		const [ann] = await reader.receive(new Promise(() => {}));

		assertEquals(ann?.hopIDs, [7, 0]);
		done();
	});

	await t.step("ANNOUNCE_END retires the Announce ID and ends the announcement", async () => {
		const { reader, violations, done } = await readerOver("/", [
			new AnnounceStartMessage({ suffix: "a" }),
			new AnnounceEndMessage({ announceId: 0 }),
		]);
		const [ann] = await reader.receive(new Promise(() => {}));
		assertExists(ann);

		await ann.ended();

		assertEquals(ann.isActive(), false);
		assertEquals(violations.reasons, []);
		done();
	});

	await t.step("ANNOUNCE_UPDATE changes the hop path in place", async () => {
		const { reader, done } = await readerOver("/", [
			new AnnounceStartMessage({ suffix: "a", hopIDs: [1] }),
			new AnnounceUpdateMessage({ announceId: 0, hopIDs: [2] }),
		], { okHop: 4 });
		const [ann] = await reader.receive(new Promise(() => {}));

		await tick();

		assertEquals(ann?.isActive(), true);
		assertEquals(ann?.hopIDs, [2, 4]);
		done();
	});

	await t.step("an announcement through this endpoint's Hop ID is discarded", async () => {
		const { reader, done } = await readerOver("/", [
			new AnnounceStartMessage({ suffix: "loop", hopIDs: [42] }),
			new AnnounceStartMessage({ suffix: "ok", hopIDs: [1] }),
		], { localHopId: 42 });

		const [ann] = await reader.receive(new Promise(() => {}));

		assertEquals(ann?.broadcastPath, "/ok");
		done();
	});

	const violationCases = [
		{
			name: "a second ANNOUNCE_START for an advertised route",
			messages: [
				new AnnounceStartMessage({ suffix: "a" }),
				new AnnounceStartMessage({ suffix: "a" }),
			],
		},
		{
			name: "ANNOUNCE_END for an unknown Announce ID",
			messages: [new AnnounceEndMessage({ announceId: 3 })],
		},
		{
			name: "ANNOUNCE_END for a retired Announce ID",
			messages: [
				new AnnounceStartMessage({ suffix: "a" }),
				new AnnounceEndMessage({ announceId: 0 }),
				new AnnounceEndMessage({ announceId: 0 }),
			],
		},
		{
			name: "ANNOUNCE_UPDATE for an unknown Announce ID",
			messages: [new AnnounceUpdateMessage({ announceId: 1 })],
		},
		{
			name: "a repeated non-zero Hop ID",
			messages: [new AnnounceStartMessage({ suffix: "a", hopIDs: [5, 0, 5] })],
		},
	];
	for (const c of violationCases) {
		await t.step(`${c.name} closes the session with a protocol violation`, async () => {
			const { reader, violations, done } = await readerOver("/", c.messages);

			await reader.context.done();

			assertEquals(violations.reasons.length, 1);
			done();
		});
	}

	await t.step("repeated zero Hop IDs are legal", async () => {
		const { reader, violations, done } = await readerOver("/", [
			new AnnounceStartMessage({ suffix: "a", hopIDs: [0, 0] }),
		]);

		const [ann] = await reader.receive(new Promise(() => {}));

		assertExists(ann);
		assertEquals(violations.reasons, []);
		done();
	});

	await t.step("an announcement before ANNOUNCE_OK closes the session", async () => {
		const buf = Buffer.make(64);
		await writeAnnouncement(buf, new AnnounceStartMessage({ suffix: "a" }));
		const violations = new FakeViolations();
		const [ctx, cancel] = withCancelCause(background());
		const reader = new AnnouncementReader(
			ctx,
			new MockStream({ readable: new MockReceiveStream({ read: openRead(buf) }) }),
			new AnnounceRequestMessage({ prefix: "/" }),
			{ onProtocolViolation: violations.report },
		);

		await reader.context.done();

		assertEquals(violations.reasons.length, 1);
		cancel(new Error("test cleanup"));
	});

	await t.step("the stream ending ends every active announcement", async (t) => {
		const cases: Record<string, {
			after?: () => Promise<[number, Error | undefined]>;
			isExpected: (err: Error | undefined) => boolean;
		}> = {
			"end of stream": { isExpected: (err) => err instanceof EOFError },
			"stream reset": {
				after: () =>
					Promise.resolve([
						0,
						new WebTransportStreamError(
							{ source: "stream", streamErrorCode: AnnounceErrorCode.InternalError },
							true,
						),
					]),
				isExpected: (err) => err instanceof AnnounceError,
			},
		};
		for (const [name, c] of Object.entries(cases)) {
			await t.step(name, async () => {
				const buf = Buffer.make(256);
				await new AnnounceOkMessage({}).encode(buf);
				await writeAnnouncement(buf, new AnnounceStartMessage({ suffix: "a" }));
				const after = c.after;
				const violations = new FakeViolations();
				const [ctx, cancel] = withCancelCause(background());
				const ar = new AnnouncementReader(
					ctx,
					new MockStream({
						readable: new MockReceiveStream({
							read: (p) => after && buf.len() === 0 ? after() : buf.read(p),
						}),
					}),
					new AnnounceRequestMessage({ prefix: "/" }),
					{ onProtocolViolation: violations.report },
				);
				const [ann] = await ar.receive(new Promise(() => {}));
				assertExists(ann);

				await ann.ended();

				const [next, err] = await ar.receive(new Promise(() => {}));
				assertEquals(next, undefined);
				assertEquals(c.isExpected(err), true);
				assertEquals(violations.reasons, []);
				cancel(new Error("test cleanup"));
			});
		}
	});

	await t.step("receive returns error when queue closed", async () => {
		const [ctx, cancel] = withCancelCause(background());
		const ar = new AnnouncementReader(
			ctx,
			new MockStream({}),
			new AnnounceRequestMessage({ prefix: "/test/" }),
		);
		await ar.close();

		const [ann, err] = await ar.receive(new Promise(() => {}));

		assertEquals(ann, undefined);
		assertInstanceOf(err, Error);
		cancel(new Error("test cleanup"));
	});
});
