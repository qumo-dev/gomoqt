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
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";
import { TrackMux } from "./track_mux.ts";
import type { BroadcastPath } from "./broadcast_path.ts";
import type { TrackWriter } from "./track_writer.ts";
import type { SendStream } from "./internal/webtransport/mod.ts";
import {
	InvalidBroadcastPathError,
	type Redirect,
	RedirectError,
	RedirectNotSupportedError,
	SessionErrorCode,
	StopRedirect,
	SubscriptionAnsweredError,
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
	assert(
		sm.parameters.some((p) => p.id === SetupParamIDs.Redirect && p.value.length === 0),
		"Redirect, version 0",
	);

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
		const session = new Session({ transport: mock, checkRedirect });
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

	await t.step("stops after ten, whatever it says", async () => {
		const paths = Array.from({ length: 11 }, (_, i) => `/hop-${i}`);
		const [, err] = await run(paths);
		assertInstanceOf(err, RedirectError);
		assertInstanceOf(err.cause, TooManyRedirectsError);
		assertEquals(err.redirect.broadcastPath, "/hop-10");
		assertEquals(calls.length, 10, "asked about the first ten only");
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

// ─── Publisher side ──────────────────────────────────────────────────────────

// An incoming Setup Stream item: the subscriber's SETUP, advertising that it
// follows redirects when follows is true.
async function subscriberSetup(follows: boolean): Promise<{ type: number; data: Uint8Array }> {
	const sm = new SetupMessage({});
	if (follows) sm.addRedirect();
	const data = await bytesOf(async (b) => {
		await writeVarint(b, UniStreamTypes.SetupStreamType);
		await sm.encode(b);
	});
	return { type: UniStreamTypes.SetupStreamType, data };
}

// An incoming Subscribe Stream item for path and name.
async function incomingSubscribe(
	path: string,
	name: string,
): Promise<{ type: number; data: Uint8Array }> {
	const data = await bytesOf(async (b) => {
		await writeVarint(b, BiStreamTypes.SubscribeStreamType);
		await new SubscribeMessage({ subscribeId: 1, broadcastPath: path, trackName: name }).encode(
			b,
		);
	});
	return { type: BiStreamTypes.SubscribeStreamType, data };
}

// HandlerMux is a TrackMux that serves every subscription with handler and
// resolves served once handler returns.
class HandlerMux extends TrackMux {
	readonly served: Promise<void>;
	#done: () => void = () => {};
	#handler: (tw: TrackWriter) => Promise<void>;

	constructor(handler: (tw: TrackWriter) => Promise<void>) {
		super();
		this.#handler = handler;
		this.served = new Promise((resolve) => (this.#done = resolve));
	}

	override async serveTrack(tw: TrackWriter): Promise<void> {
		try {
			await this.#handler(tw);
		} finally {
			this.#done();
		}
	}
}

// serve runs one incoming subscription to /.hls/acme/cam video through handler
// on a session whose subscriber's SETUP says follows (or that sends none, for
// undefined), and returns what the session wrote back on the subscribe stream.
async function serve(
	follows: boolean | undefined,
	handler: (tw: TrackWriter) => Promise<void>,
): Promise<Uint8Array> {
	const mux = new HandlerMux(handler);
	const mock = new MockWebTransportSession({
		acceptUniStreamData: follows === undefined ? [] : [await subscriberSetup(follows)],
		acceptStreamData: [await incomingSubscribe("/.hls/acme/cam", "video")],
	});
	const session = new Session({ transport: mock, mux });
	await session.ready;
	await mux.served;
	await session.close();
	return concat(mock.acceptStreamWrittenData[0] ?? []);
}

// sentRedirect decodes the SUBSCRIBE_REDIRECT that starts written and returns
// it, with how many bytes of written follow it.
async function sentRedirect(
	written: Uint8Array,
): Promise<{ msg: SubscribeRedirectMessage; rest: number }> {
	const r = await readerOf(written);
	const [type, typeLen] = await readVarint(r);
	assertEquals(type, MESSAGE_TYPE_SUBSCRIBE_REDIRECT);
	const msg = new SubscribeRedirectMessage({});
	assertEquals(await msg.decode(r), undefined);
	const encoded = await bytesOf((b) => msg.encode(b));
	return { msg, rest: written.length - typeLen - encoded.length };
}

Deno.test("TrackWriter.redirect sends one SUBSCRIBE_REDIRECT to the target", async () => {
	let err: Error | undefined;

	const written = await serve(true, async (tw) => {
		err = await tw.redirect({ broadcastPath: "/acme/cam", trackName: "video-hd" });
	});

	assertEquals(err, undefined);
	const { msg, rest } = await sentRedirect(written);
	assertEquals([msg.connectUri, msg.broadcastPath, msg.trackName], ["", "/acme/cam", "video-hd"]);
	assertEquals(rest, 0);
});

Deno.test("TrackWriter.redirect keeps the subscriber's track name when none or an empty one is given", async (t) => {
	const cases: Record<string, { broadcastPath: BroadcastPath; trackName?: string }> = {
		"omitted": { broadcastPath: "/acme/cam" },
		"empty": { broadcastPath: "/acme/cam", trackName: "" },
	};
	for (const [name, to] of Object.entries(cases)) {
		await t.step(name, async () => {
			const written = await serve(true, async (tw) => {
				await tw.redirect(to);
			});

			assertEquals((await sentRedirect(written)).msg.trackName, "video");
		});
	}
});

Deno.test("TrackWriter.openGroup fails after a redirect", async () => {
	let openErr: Error | undefined;

	await serve(true, async (tw) => {
		await tw.redirect({ broadcastPath: "/acme/cam" });
		[, openErr] = await tw.openGroup();
	});

	assertExists(openErr);
});

Deno.test("TrackWriter.close sends nothing after a redirect", async () => {
	const written = await serve(true, async (tw) => {
		await tw.redirect({ broadcastPath: "/acme/cam" });
		await tw.close();
	});

	assertEquals((await sentRedirect(written)).rest, 0, "no SUBSCRIBE_END after the redirect");
});

Deno.test("TrackWriter.redirect refuses a subscriber that does not follow redirects", async () => {
	let err: Error | undefined;

	const written = await serve(false, async (tw) => {
		err = await tw.redirect({ broadcastPath: "/acme/cam" });
	});

	assertInstanceOf(err, RedirectNotSupportedError);
	assertEquals(written.length, 0);
});

Deno.test("TrackWriter.redirect fails once SUBSCRIBE_OK went out", async () => {
	let err: Error | undefined;

	await serve(true, async (tw) => {
		const [group] = await tw.openGroup();
		await group?.close();
		err = await tw.redirect({ broadcastPath: "/acme/cam" });
	});

	assertInstanceOf(err, SubscriptionAnsweredError);
});

Deno.test("TrackWriter.redirect started alongside openGroup leaves one answer on the stream", async () => {
	let openErr: Error | undefined;
	let redirectErr: Error | undefined;

	const written = await serve(true, async (tw) => {
		// The redirect first waits for the SETUP, so SUBSCRIBE_OK claims the
		// answer; the redirect must then write nothing.
		const redirected = tw.redirect({ broadcastPath: "/acme/cam" });
		const opened = tw.openGroup();
		redirectErr = await redirected;
		[, openErr] = await opened;
	});

	assertEquals(openErr, undefined);
	assertInstanceOf(redirectErr, SubscriptionAnsweredError);
	const r = await readerOf(written);
	const [type, typeLen] = await readVarint(r);
	assertEquals(type, MESSAGE_TYPE_SUBSCRIBE_OK);
	const ok = new SubscribeOkMessage({});
	assertEquals(await ok.decode(r), undefined);
	const okLen = (await bytesOf((b) => ok.encode(b))).length;
	assertEquals(written.length, typeLen + okLen, "nothing but SUBSCRIBE_OK");
});

Deno.test("TrackWriter.redirect rejects a path that is not one", async () => {
	let err: Error | undefined;

	const written = await serve(true, async (tw) => {
		err = await tw.redirect({ broadcastPath: "acme/cam" as BroadcastPath });
	});

	assertInstanceOf(err, InvalidBroadcastPathError);
	assertEquals(err.path, "acme/cam");
	assertEquals(written.length, 0);
});

Deno.test("TrackWriter.redirect stops waiting for SETUP when its signal aborts", async () => {
	const reason = new Error("gave up");
	let err: Error | undefined;

	await serve(undefined, async (tw) => {
		err = await tw.redirect({ broadcastPath: "/acme/cam" }, {
			signal: AbortSignal.abort(reason),
		});
	});

	assertEquals(err, reason);
});

Deno.test("SetupMessage.followsRedirect reads one varint version", async (t) => {
	const cases: Record<string, { value: number[]; want: boolean }> = {
		"empty is version 0": { value: [], want: true },
		"version 0": { value: [0x00], want: true },
		"a later version": { value: [0x02], want: true },
		"a two-byte version": { value: [0x40, 0x01], want: true },
		"truncated varint": { value: [0x40], want: false },
		"trailing bytes": { value: [0x01, 0x01], want: false },
	};
	for (const [name, tt] of Object.entries(cases)) {
		await t.step(name, () => {
			const sm = new SetupMessage({
				parameters: [{ id: SetupParamIDs.Redirect, value: new Uint8Array(tt.value) }],
			});

			assertEquals(sm.followsRedirect(), tt.want);
		});
	}
});
