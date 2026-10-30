import { assert, assertEquals, assertExists, assertInstanceOf } from "@std/assert";
import { Buffer } from "@okdaichi/golikejs/bytes";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import {
	readVarint,
	SetupMessage,
	SetupParamIDs,
	SubscribeMessage,
	SubscribeOkMessage,
	SubscribeRedirectMessage,
	writeVarint,
} from "./internal/message/mod.ts";
import { MESSAGE_TYPE_SUBSCRIBE_OK, MESSAGE_TYPE_SUBSCRIBE_REDIRECT } from "./subscribe_stream.ts";
import { UniStreamTypes } from "./stream_type.ts";
import type { SendStream } from "./internal/webtransport/mod.ts";
import {
	type Redirect,
	RedirectError,
	SessionErrorCode,
	StopRedirect,
	TooManyRedirectsError,
} from "./error.ts";

// The SUBSCRIBE_REDIRECT body Go's message.SubscribeRedirectMessage encodes
// for {ConnectURI: "moqt://b/", BroadcastPath: "/real", TrackName: "video"}
// (TestSubscribeRedirectMessage_GoldenBytes pins the same bytes), so this
// pins interop with the Go implementation.
const GOLDEN_REDIRECT = new Uint8Array([
	0x16, // message length 22
	0x09,
	...new TextEncoder().encode("moqt://b/"),
	0x05,
	...new TextEncoder().encode("/real"),
	0x05,
	...new TextEncoder().encode("video"),
]);

async function bytesOf(write: (b: Buffer) => Promise<unknown>): Promise<Uint8Array> {
	const buf = Buffer.make(64);
	await write(buf);
	return buf.bytes();
}

function redirectResponse(init: { connectUri?: string; broadcastPath: string; trackName: string }) {
	return bytesOf(async (b) => {
		await writeVarint(b, MESSAGE_TYPE_SUBSCRIBE_REDIRECT);
		await new SubscribeRedirectMessage(init).encode(b);
	});
}

function okResponse(group = 1) {
	return bytesOf(async (b) => {
		await writeVarint(b, MESSAGE_TYPE_SUBSCRIBE_OK);
		await new SubscribeOkMessage({ group }).encode(b);
	});
}

// readerOf returns a Buffer to read bytes from.
async function readerOf(bytes: Uint8Array): Promise<Buffer> {
	const buf = Buffer.make(bytes.length);
	await buf.write(bytes);
	return buf;
}

function concat(chunks: Uint8Array[]): Uint8Array {
	const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
	let off = 0;
	for (const c of chunks) {
		out.set(c, off);
		off += c.length;
	}
	return out;
}

// The SUBSCRIBE the session wrote on an opened stream, after its stream type.
async function sentSubscribe(written: Uint8Array[]): Promise<SubscribeMessage> {
	const buf = await readerOf(concat(written));
	const [, , typeErr] = await readVarint(buf);
	assertEquals(typeErr, undefined);
	const msg = new SubscribeMessage({});
	assertEquals(await msg.decode(buf), undefined);
	return msg;
}

// ─── Wire ────────────────────────────────────────────────────────────────────

Deno.test("SubscribeRedirectMessage encodes the bytes Go encodes", async () => {
	const got = await bytesOf((b) =>
		new SubscribeRedirectMessage({
			connectUri: "moqt://b/",
			broadcastPath: "/real",
			trackName: "video",
		})
			.encode(b)
	);
	assertEquals(got, GOLDEN_REDIRECT);
});

Deno.test("SubscribeRedirectMessage decodes the bytes Go encodes", async () => {
	const msg = new SubscribeRedirectMessage({});
	assertEquals(await msg.decode(await readerOf(GOLDEN_REDIRECT)), undefined);
	assertEquals(msg.connectUri, "moqt://b/");
	assertEquals(msg.broadcastPath, "/real");
	assertEquals(msg.trackName, "video");
});

Deno.test("SubscribeRedirectMessage rejects a body that does not match its length", async (t) => {
	const cases: Record<string, number[]> = {
		"missing track name": [0x02, 0x00, 0x00],
		"string past body": [0x03, 0x00, 0x05, 0x00],
		"trailing bytes": [0x04, 0x00, 0x00, 0x00, 0xff],
	};
	for (const [name, bytes] of Object.entries(cases)) {
		await t.step(name, async () => {
			const msg = new SubscribeRedirectMessage({});
			assertExists(await msg.decode(await readerOf(new Uint8Array(bytes))));
		});
	}
});

Deno.test("SetupMessage.followsRedirect reads the version", async (t) => {
	const cases: Record<string, { value: number[] | undefined; want: boolean }> = {
		"absent": { value: undefined, want: false },
		"empty is version 0": { value: [], want: true },
		"version 0": { value: [0x00], want: true },
		"a later version": { value: [0x02], want: true },
		"truncated varint": { value: [0x40], want: false },
		"trailing bytes": { value: [0x01, 0x01], want: false },
	};
	for (const [name, tt] of Object.entries(cases)) {
		await t.step(name, () => {
			const sm = new SetupMessage({
				parameters: tt.value === undefined
					? []
					: [{ id: SetupParamIDs.Redirect, value: new Uint8Array(tt.value) }],
			});
			assertEquals(sm.followsRedirect(), tt.want);
		});
	}
});

// ─── Session ─────────────────────────────────────────────────────────────────

// SetupCapture records what the session writes on the streams it opens one
// way, which carry its SETUP.
class SetupCapture extends MockWebTransportSession {
	uniWritten: Uint8Array[] = [];

	override async openUniStream(
		options?: { signal?: AbortSignal },
	): Promise<[SendStream, undefined] | [undefined, Error]> {
		const [stream, err] = await super.openUniStream(options);
		if (err) return [undefined, err];
		const write = stream.write.bind(stream);
		stream.write = async (p: Uint8Array) => {
			this.uniWritten.push(new Uint8Array(p));
			return await write(p);
		};
		return [stream, undefined];
	}
}

Deno.test("Session SETUP advertises that it follows redirects", async () => {
	const mock = new SetupCapture({});
	const session = new Session({ transport: mock });
	await session.ready;
	await new Promise((resolve) => setTimeout(resolve, 10));

	const buf = await readerOf(concat(mock.uniWritten));
	const [type] = await readVarint(buf);
	assertEquals(type, UniStreamTypes.SetupStreamType);
	const sm = new SetupMessage({});
	assertEquals(await sm.decode(buf), undefined);
	assertEquals(sm.followsRedirect(), true);

	await session.close();
});

Deno.test("Session.subscribe follows redirects on the same session", async (t) => {
	await t.step("one: subscribes to the target with the same parameters", async () => {
		const mock = new MockWebTransportSession({
			openStreamResponses: [
				await redirectResponse({ broadcastPath: "/real", trackName: "audio" }),
				await okResponse(),
			],
		});
		const session = new Session({ transport: mock });
		await session.ready;

		const [track, err] = await session.subscribe("/alias", "video", {
			priority: 7,
			ordered: true,
			maxLatency: 100,
			startGroup: 0,
			endGroup: 0,
		});

		assertEquals(err, undefined);
		assertExists(track);
		assertEquals(track.broadcastPath, "/real");
		assertEquals(track.trackName, "audio");
		assertEquals(mock.openStreamWrittenData.length, 2);
		const second = await sentSubscribe(mock.openStreamWrittenData[1]!);
		assertEquals(second.broadcastPath, "/real");
		assertEquals(second.trackName, "audio");
		assertEquals(second.subscriberPriority, 7);
		assertEquals(second.subscriberOrdered, 1);
		assertEquals(second.subscriberMaxLatency, 100);
		await session.close();
	});

	await t.step("two in a row", async () => {
		const mock = new MockWebTransportSession({
			openStreamResponses: [
				await redirectResponse({ broadcastPath: "/alias", trackName: "video" }),
				await redirectResponse({ broadcastPath: "/real", trackName: "video" }),
				await okResponse(),
			],
		});
		const session = new Session({ transport: mock });
		await session.ready;

		const [track, err] = await session.subscribe("/alias-of-alias", "video");

		assertEquals(err, undefined);
		assertEquals(track?.broadcastPath, "/real");
		await session.close();
	});
});

Deno.test("Session.subscribe returns a RedirectError for what it does not follow", async (t) => {
	const cases: Record<string, {
		responses: Array<{ connectUri?: string; broadcastPath: string; trackName: string }>;
		wantTo: Redirect;
		wantCause?: (cause: unknown) => void;
		wantStreams: number;
	}> = {
		"another session": {
			responses: [{
				connectUri: "moqt://relay-b.example/",
				broadcastPath: "/real",
				trackName: "video",
			}],
			wantTo: { uri: "moqt://relay-b.example/", broadcastPath: "/real", trackName: "video" },
			wantStreams: 1,
		},
		"a third redirect": {
			responses: [
				{ broadcastPath: "/b", trackName: "video" },
				{ broadcastPath: "/a", trackName: "video" },
				{ broadcastPath: "/b", trackName: "video" },
			],
			wantTo: { uri: "", broadcastPath: "/b", trackName: "video" },
			wantCause: (cause) => assertInstanceOf(cause, TooManyRedirectsError),
			wantStreams: 3,
		},
		"a path that is not one": {
			responses: [{ broadcastPath: "real", trackName: "video" }],
			wantTo: { uri: "", broadcastPath: "real", trackName: "video" },
			wantCause: (cause) => assertInstanceOf(cause, Error),
			wantStreams: 1,
		},
	};
	for (const [name, tt] of Object.entries(cases)) {
		await t.step(name, async () => {
			const responses = await Promise.all(tt.responses.map((r) => redirectResponse(r)));
			const mock = new MockWebTransportSession({ openStreamResponses: responses });
			const session = new Session({ transport: mock });
			await session.ready;

			const [track, err] = await session.subscribe("/a", "video");

			assertEquals(track, undefined);
			assertInstanceOf(err, RedirectError);
			assertEquals(err.redirect, tt.wantTo);
			if (tt.wantCause) {
				tt.wantCause(err.cause);
			} else {
				assertEquals(err.cause, undefined);
			}
			assertEquals(mock.openStreamWrittenData.length, tt.wantStreams);
			await session.close();
		});
	}
});

Deno.test("Session.subscribe asks checkRedirect", async (t) => {
	const veto = new Error("vetoed");
	const calls: Array<{ to: string; via: string[] }> = [];
	const checkRedirect = (to: Redirect, via: Redirect[]): Error | undefined => {
		calls.push({ to: to.broadcastPath, via: via.map((v) => v.broadcastPath) });
		// Changing via must not change the history of later calls.
		for (const v of via) v.broadcastPath = "/tampered";
		if (to.broadcastPath === "/stop") return StopRedirect;
		if (to.broadcastPath === "/real" && via.length > 0) return veto;
		return undefined;
	};

	const run = async (paths: string[], last?: Uint8Array) => {
		calls.length = 0;
		const responses = await Promise.all(
			paths.map((p) => redirectResponse({ broadcastPath: p, trackName: "video" })),
		);
		if (last) responses.push(last);
		const mock = new MockWebTransportSession({ openStreamResponses: responses });
		const session = new Session({ transport: mock, options: { checkRedirect } });
		await session.ready;
		const result = await session.subscribe("/start", "video");
		await session.close();
		return result;
	};

	await t.step("follows past the default limit", async () => {
		const [track, err] = await run(["/a", "/b", "/c"], await okResponse());
		assertEquals(err, undefined);
		assertEquals(track?.broadcastPath, "/c");
		assertEquals(calls, [
			{ to: "/a", via: [] },
			{ to: "/b", via: ["/a"] },
			{ to: "/c", via: ["/a", "/b"] },
		]);
	});

	await t.step("vetoes: the error is the cause", async () => {
		const [, err] = await run(["/alias", "/real"]);
		assertInstanceOf(err, RedirectError);
		assertEquals(err.redirect.broadcastPath, "/real");
		assertEquals(err.cause, veto);
	});

	await t.step("stops: returned as itself", async () => {
		const [, err] = await run(["/stop"]);
		assertInstanceOf(err, RedirectError);
		assertEquals(err.redirect.broadcastPath, "/stop");
		assertEquals(err.cause, undefined);
	});
});

Deno.test("Session closes on a SUBSCRIBE_REDIRECT after SUBSCRIBE_OK", async () => {
	const late = concat([
		await okResponse(),
		await redirectResponse({ broadcastPath: "/real", trackName: "video" }),
	]);
	const mock = new MockWebTransportSession({ openStreamResponses: [late] });
	const session = new Session({ transport: mock });
	await session.ready;

	const [track, err] = await session.subscribe("/alias", "video");
	assertEquals(err, undefined);
	assert(track);

	const info = await session.closed;
	assertEquals(info.code, SessionErrorCode.ProtocolViolation);
});
