import { assertEquals, assertInstanceOf } from "@std/assert";
import { WebTransportSession } from "./connection.ts";
import { StreamConnError } from "./error.ts";
import { SendStream } from "./send_stream.ts";
import { ContextCancelledError } from "@okdaichi/golikejs/context";

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

Deno.test("WebTransportSession.openUniStream with a pre-aborted signal does not open a stream", async () => {
	let createCalled = false;
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		createUnidirectionalStream() {
			createCalled = true;
			return new Promise<WritableStream<Uint8Array>>(() => {});
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	ac.abort(new Error("nope"));
	const [stream, err] = await session.openUniStream(ac.signal);
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals((err as Error).message, "nope");
	assertEquals(createCalled, false);
});

Deno.test("WebTransportSession.openUniStream happy path with a signal returns a SendStream", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		async createUnidirectionalStream() {
			return new WritableStream<Uint8Array>();
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	const [stream, err] = await session.openUniStream(ac.signal);
	assertEquals(err, undefined);
	assertInstanceOf(stream, SendStream);
});

Deno.test("WebTransportSession.openUniStream abort mid-wait abandons the late-resolving stream", async () => {
	// A real WritableStream instance (so abandonRawStream's instanceof branch
	// is taken) with a shadowed abort so the cleanup is observable.
	let lateAborted = false;
	const lateStream = new WritableStream<Uint8Array>();
	Object.defineProperty(lateStream, "abort", {
		value: (_reason?: unknown) => {
			lateAborted = true;
			return Promise.resolve();
		},
		configurable: true,
	});

	let resolveCreate!: (s: WritableStream<Uint8Array>) => void;
	const createPromise = new Promise<WritableStream<Uint8Array>>((r) => {
		resolveCreate = r;
	});
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		createUnidirectionalStream() {
			return createPromise;
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	const openP = session.openUniStream(ac.signal);
	ac.abort(new Error("timeout"));

	const [stream, err] = await openP;
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals((err as Error).message, "timeout");

	// The WT open may still settle after the abort; the late stream must be
	// abandoned rather than orphaned.
	assertEquals(lateAborted, false);
	resolveCreate(lateStream);
	await Promise.resolve();
	await Promise.resolve();
	assertEquals(lateAborted, true);
});

Deno.test("WebTransportSession.openStream with a pre-aborted signal does not open a stream", async () => {
	let createCalled = false;
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		createBidirectionalStream() {
			createCalled = true;
			return new Promise(() => {});
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	ac.abort(new Error("nope"));
	const [stream, err] = await session.openStream(ac.signal);
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals((err as Error).message, "nope");
	assertEquals(createCalled, false);
});

Deno.test("WebTransportSession.openUniStream pre-aborted with a non-Error reason returns a ContextCancelledError", async () => {
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		createUnidirectionalStream() {
			return new Promise<WritableStream<Uint8Array>>(() => {});
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	// A non-Error reason falls back to ContextCancelledError.
	ac.abort("a string reason");
	const [stream, err] = await session.openUniStream(ac.signal);
	assertEquals(stream, undefined);
	assertInstanceOf(err, ContextCancelledError);
});

Deno.test("WebTransportSession.openStream abort mid-wait abandons the late-resolving bidirectional stream", async () => {
	let writableAborted = false;
	let readableCancelled = false;
	const lateBidi = {
		writable: {
			abort: (_reason?: unknown) => {
				writableAborted = true;
				return Promise.resolve();
			},
		},
		readable: {
			cancel: () => {
				readableCancelled = true;
				return Promise.resolve();
			},
		},
	};

	let resolveCreate!: (s: unknown) => void;
	const createPromise = new Promise<unknown>((r) => {
		resolveCreate = r;
	});
	const mock = {
		incomingBidirectionalStreams: new ReadableStream({ start(_c) {} }),
		incomingUnidirectionalStreams: new ReadableStream({ start(_c) {} }),
		ready: Promise.resolve(),
		closed: Promise.resolve({ closeCode: undefined, reason: undefined }),
		createBidirectionalStream() {
			return createPromise;
		},
	} as unknown as WebTransport;

	const session = new WebTransportSession(mock);
	const ac = new AbortController();
	const openP = session.openStream(ac.signal);
	ac.abort(new Error("timeout"));

	const [stream, err] = await openP;
	assertEquals(stream, undefined);
	assertInstanceOf(err, Error);
	assertEquals((err as Error).message, "timeout");

	assertEquals(writableAborted, false);
	resolveCreate(lateBidi);
	await Promise.resolve();
	await Promise.resolve();
	assertEquals(writableAborted, true);
	assertEquals(readableCancelled, true);
});
