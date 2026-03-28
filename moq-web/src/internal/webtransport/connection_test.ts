import { assertEquals, assertInstanceOf } from "@std/assert";
import { WebTransportSession } from "./connection.ts";
import { StreamConnError } from "./error.ts";

class FailingMockWebTransport {
	ready = Promise.resolve();
	closed = Promise.resolve({ closeCode: 123, reason: "fail" });
	incomingBidirectionalStreams = new ReadableStream({ start(_c) {} });
	incomingUnidirectionalStreams = new ReadableStream({ start(_c) {} });
	async createBidirectionalStream() {
		const err = { source: "session" as const } as any; // not an Error
		throw err;
	}
}

Deno.test("WebTransportSession.openStream maps session-sourced WebTransportError to StreamConnError", async () => {
	const session = new WebTransportSession(
		(new FailingMockWebTransport()) as any,
	);
	const [stream, err] = await session.openStream();
	assertEquals(stream, undefined);
	assertInstanceOf(err, StreamConnError);
});

Deno.test("WebTransportSession.acceptStream/acceptUniStream return error when incoming readers are done", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({
			start(controller) {
				controller.close();
			},
		}),
		incomingUnidirectionalStreams: new ReadableStream({
			start(controller) {
				controller.close();
			},
		}),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
	} as any;

	const session = new WebTransportSession(mock);

	const [s1, e1] = await session.acceptStream();
	assertEquals(s1, undefined);
	assertInstanceOf(e1, Error);

	const [s2, e2] = await session.acceptUniStream();
	assertEquals(s2, undefined);
	assertInstanceOf(e2, Error);
});

Deno.test("WebTransportSession.openStream returns thrown Error from createBidirectionalStream", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createBidirectionalStream() {
			throw new Error("connection failed");
		},
	} as any;

	const session = new WebTransportSession(mock);
	const [stream, err] = await session.openStream();
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals(err?.message, "connection failed");
});

Deno.test("WebTransportSession.openStream preserves non-session WebTransportError", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createBidirectionalStream() {
			const err = { source: "stream" } as any;
			throw err;
		},
	} as any;

	const session = new WebTransportSession(mock);
	const [stream, err] = await session.openStream();
	assertEquals(stream, undefined);
	assertEquals((err as any).source, "stream");
});

Deno.test("WebTransportSession.openUniStream returns thrown error from createUnidirectionalStream", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createUnidirectionalStream() {
			throw new Error("uni stream failed");
		},
	} as any;

	const session = new WebTransportSession(mock);
	const [stream, err] = await session.openUniStream();
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals(err?.message, "uni stream failed");
});

Deno.test("WebTransportSession.openStream returns Stream with readable/writable wrappers", async () => {
	const mockWritableStream = {
		getWriter() {
			return {
				ready: Promise.resolve(),
				write: async () => {},
				close: async () => {},
				abort: async () => {},
				releaseLock: () => {},
				closed: Promise.resolve(),
			};
		},
	};
	const mockReadableStream = {
		getReader() {
			return {
				read: async () => ({ done: true, value: undefined }),
				cancel: async () => {},
				releaseLock: () => {},
			};
		},
	};

	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createBidirectionalStream() {
			return {
				readable: mockReadableStream,
				writable: mockWritableStream,
			};
		},
	} as any;

	const session = new WebTransportSession(mock);
	const [stream, err] = await session.openStream();
	assertEquals(err, undefined);
	assertEquals(typeof stream?.readable, "object");
	assertEquals(typeof stream?.writable, "object");
	assertEquals(typeof stream?.readable?.cancel, "function");
	assertEquals(typeof stream?.writable?.close, "function");
});

Deno.test("WebTransportSession.openUniStream returns SendStream wrapper", async () => {
	const mockWritableStream = {
		getWriter() {
			return {
				ready: Promise.resolve(),
				write: async () => {},
				close: async () => {},
				abort: async () => {},
				releaseLock: () => {},
				closed: Promise.resolve(),
			};
		},
	};

	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createUnidirectionalStream() {
			return mockWritableStream;
		},
	} as any;

	const session = new WebTransportSession(mock);
	const [stream, err] = await session.openUniStream();
	assertEquals(err, undefined);
	assertEquals(typeof stream?.close, "function");
	assertEquals(typeof stream?.cancel, "function");
	assertEquals(typeof stream?.closed, "function");
});

Deno.test("WebTransportSession.close closes transport", async () => {
	let closeCalled = false;
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		close(_info?: any) {
			closeCalled = true;
		},
	} as any;

	const session = new WebTransportSession(mock);
	session.close({ closeCode: 0, reason: "test" });
	assertEquals(closeCalled, true);
});

Deno.test("WebTransportSession.ready and closed proxy underlying transport promises", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: 0, reason: "closed" }),
	} as any;

	const session = new WebTransportSession(mock);
	await session.ready;
	const closeInfo = await session.closed;
	assertEquals(closeInfo.closeCode, 0);
	assertEquals(closeInfo.reason, "closed");
});
