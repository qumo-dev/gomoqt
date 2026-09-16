import { assertEquals } from "@std/assert";
import { background } from "@okdaichi/golikejs/context";
import { AnnouncementReader } from "./announce_stream.ts";
import { AnnounceRequestMessage } from "./internal/message/mod.ts";
import { MockReceiveStream, MockStream } from "./mock_stream_test.ts";
import { Session } from "./session.ts";
import { MockWebTransportSession } from "./session_test.ts";
import { BiStreamTypes } from "./stream_type.ts";
import { ProbeErrorCode } from "./error.ts";

// A malformed ANNOUNCE_OK must surface as an error from receive() rather than
// hanging forever on an open, empty queue (the bug fixed in this PR).
Deno.test("AnnouncementReader surfaces a malformed ANNOUNCE_OK", async () => {
	// A single garbage byte cannot parse as an ANNOUNCE_OK body.
	const mockStream = new MockStream({
		readable: new MockReceiveStream({
			read: (_p: Uint8Array) =>
				Promise.resolve([1, undefined] as [number, Error | undefined]),
		}),
	});

	const reader = new AnnouncementReader(
		background(),
		mockStream,
		new AnnounceRequestMessage({ prefix: "/x/" }),
	);

	// Give the async ANNOUNCE_OK decode a tick to fail and run #fail.
	const [ann, err] = await reader.receive(new Promise<void>(() => {}));
	assertEquals(ann, undefined);
	assertEquals(err instanceof Error, true);
});

// An unadvertised Probe Stream (local capability None) is reset with
// ProbeErrorCode.NotSupported, matching Go and the spec.
Deno.test("Session resets an unadvertised Probe Stream with NotSupported", async () => {
	// No `stats` option ⇒ the mock exposes no getStats ⇒ the session advertises
	// ProbeLevel.None and must reset an incoming Probe Stream.
	let resetCode: number | undefined;
	const mock = new MockWebTransportSession({
		acceptStreamData: [{
			type: BiStreamTypes.ProbeStreamType,
			data: new Uint8Array([BiStreamTypes.ProbeStreamType]),
		}],
	});
	// Capture the reset code applied to the accepted Probe stream.
	const origAcceptStream = mock.acceptStream.bind(mock);
	mock.acceptStream = async () => {
		const [s, e] = await origAcceptStream();
		if (s) {
			const origCancel = s.readable.cancel.bind(s.readable);
			s.readable.cancel = async (code?: number) => {
				resetCode = code;
				return await origCancel(code ?? 0);
			};
		}
		return [s, e] as never;
	};

	const session = new Session({ transport: mock });
	await session.ready;
	// The handler runs async on the bi-stream listener.
	for (let i = 0; i < 100 && resetCode === undefined; i++) {
		await new Promise((r) => setTimeout(r, 5));
	}
	assertEquals(resetCode, ProbeErrorCode.NotSupported);
	await session.close();
});
