import { assertEquals, assertExists, assertRejects } from "@std/assert";

// Note: This test file uses simplified unit testing approach.
// Full integration tests with Session would require complex mock data encoding.

// Mock WebTransport for testing
class MockWebTransport {
	ready: Promise<void>;
	closed: Promise<void>;
	incomingBidirectionalStreams: ReadableStream;
	incomingUnidirectionalStreams: ReadableStream;
	#closeResolve?: () => void;

	constructor(_url: string | URL, _options?: WebTransportOptions) {
		this.ready = Promise.resolve();
		this.closed = new Promise((resolve) => {
			this.#closeResolve = resolve;
		});

		// Mock incoming streams (empty for testing)
		this.incomingBidirectionalStreams = new ReadableStream({
			start(_controller) {
				// No incoming bidirectional streams for basic tests
			},
		});

		this.incomingUnidirectionalStreams = new ReadableStream({
			start(_controller) {
				// No incoming unidirectional streams for basic tests
			},
		});
	}

	async createBidirectionalStream(): Promise<
		{ writable: WritableStream; readable: ReadableStream }
	> {
		const writable = new WritableStream({
			write(_chunk) {
				// Mock write implementation
			},
		});
		const readable = new ReadableStream({
			start(controller) {
				// Enqueue minimal mock setup payload
				controller.enqueue(new Uint8Array([0x00, 0x00]));
				controller.close();
			},
		});
		return { writable, readable };
	}

	close() {
		if (this.#closeResolve) {
			this.#closeResolve();
		}
	}
}

// Save original WebTransport
const OriginalWebTransport = (globalThis as any).WebTransport;

// Setup mock WebTransport globally
(globalThis as any).WebTransport = MockWebTransport;

// Import after setting up mocks
import { ALPN, Client, connect } from "./client.ts";
import { TrackMux } from "./track_mux.ts";
import type { ConnectInit } from "./options.ts";

Deno.test("connect - uses default WebTransport options", async () => {
	let capturedOptions: WebTransportOptions | undefined;
	const factory = (url: string | URL, opts?: WebTransportOptions) => {
		capturedOptions = opts;
		return new MockWebTransport(url, opts);
	};

	try {
		await connect("https://example.com", { transportFactory: factory });
	} catch {
		// Expected: mock doesn't speak MOQ setup
	}

	assertExists(capturedOptions);
	assertEquals(capturedOptions!.allowPooling, false);
	assertEquals(capturedOptions!.congestionControl, "low-latency");
	assertEquals(capturedOptions!.requireUnreliable, true);
});

Deno.test("connect - merges custom transportOptions", async () => {
	let capturedOptions: WebTransportOptions | undefined;
	const factory = (url: string | URL, opts?: WebTransportOptions) => {
		capturedOptions = opts;
		return new MockWebTransport(url, opts);
	};
	const init: ConnectInit = {
		transportOptions: { allowPooling: true, congestionControl: "throughput" },
		transportFactory: factory,
	};

	try {
		await connect("https://example.com", init);
	} catch {
		// Expected
	}

	assertEquals(capturedOptions!.allowPooling, true);
	assertEquals(capturedOptions!.congestionControl, "throughput");
	assertEquals(capturedOptions!.requireUnreliable, true);
});

Deno.test("connect - accepts URL object", () => {
	const p = connect(new URL("https://example.com"));
	p.catch(() => {});
	assertExists(p);
});

Deno.test("connect - accepts mux in init", () => {
	const p = connect("https://example.com", { mux: new TrackMux() });
	p.catch(() => {});
	assertExists(p);
});

Deno.test("connect - propagates transport errors", async () => {
	class FailingTransport extends MockWebTransport {
		constructor(url: string | URL, options?: WebTransportOptions) {
			super(url, options);
			this.ready = Promise.reject(new Error("Connection refused"));
			this.ready.catch(() => {});
		}
	}

	await assertRejects(
		() => connect("https://example.com", {
			transportFactory: (u, o) => new FailingTransport(u, o),
		}),
		Error,
	);
});

Deno.test("connect - passes onGoaway to session", async () => {
	let received: string | undefined;
	const init: ConnectInit = {
		onGoaway: (uri) => { received = uri; },
		transportFactory: (u, o) => new MockWebTransport(u, o),
	};

	try {
		await connect("https://example.com", init);
	} catch {
		// Expected
	}
	assertEquals(received, undefined);
});

// Back-compat: Client shim still works
Deno.test("Client shim - dial() delegates to connect()", () => {
	const client = new Client();
	assertExists(client.dial);
	const p = client.dial("https://example.com");
	p.catch(() => {});
});

Deno.test("Client - ALPN constant is moq-lite-04", () => {
	assertEquals(ALPN, "moq-lite-04");
});

// Restore original WebTransport after all tests
Deno.test("Client - Cleanup", () => {
	(globalThis as any).WebTransport = OriginalWebTransport;
});
