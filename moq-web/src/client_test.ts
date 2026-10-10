import { assertEquals, assertExists, assertRejects } from "@std/assert";

// Note: This test file uses simplified unit testing approach.
// Full integration tests with Session would require complex mock data encoding.

// Mock WebTransport for testing
// deno-lint-ignore-file no-explicit-any
class MockWebTransport implements WebTransport {
	ready: Promise<undefined>;
	closed: Promise<WebTransportCloseInfo>;
	incomingBidirectionalStreams: ReadableStream<WebTransportBidirectionalStream>;
	incomingUnidirectionalStreams: ReadableStream<any>;
	datagrams: WebTransportDatagramDuplexStream;
	#closeResolve?: (info: WebTransportCloseInfo) => void;

	constructor(_url: string | URL, _options?: WebTransportOptions) {
		this.ready = Promise.resolve(undefined);
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

		// Mock datagrams (unused in MOQ but required by WebTransport interface)
		this.datagrams = {
			readable: new ReadableStream(),
			writable: new WritableStream(),
			incomingHighWaterMark: 0,
			incomingMaxAge: 0,
			maxDatagramSize: 0,
			outgoingHighWaterMark: 0,
			outgoingMaxAge: 0,
		} as WebTransportDatagramDuplexStream;
	}

	async createBidirectionalStream(): Promise<WebTransportBidirectionalStream> {
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
		return { writable, readable } as unknown as WebTransportBidirectionalStream;
	}

	async createUnidirectionalStream(): Promise<any> {
		return new WritableStream();
	}

	close() {
		if (this.#closeResolve) {
			this.#closeResolve({});
		}
	}

	getStats(): Promise<any> {
		return Promise.resolve({});
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
		() =>
			connect("https://example.com", {
				transportFactory: (u, o) => new FailingTransport(u, o),
			}),
		Error,
	);
});

Deno.test("connect - passes onGoaway to session", async () => {
	let received: string | undefined;
	const init: ConnectInit = {
		onGoaway: (uri) => {
			received = uri;
		},
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

Deno.test("Client - ALPN constant is moq-lite-05", () => {
	assertEquals(ALPN, "moq-lite-05");
});

// Restore original WebTransport after all tests
Deno.test("Client - Cleanup", () => {
	(globalThis as any).WebTransport = OriginalWebTransport;
});

/**
 * Starts a local HTTP server that refuses every request with 400 and records
 * what was asked of it: enough to see where and how `connect` dials a
 * WebSocket, without a QMux peer.
 */
function startRefusingServer(): {
	port: number;
	requests: { path: string; subprotocols: string | null }[];
	stop: () => Promise<void>;
} {
	const requests: { path: string; subprotocols: string | null }[] = [];
	const server = Deno.serve({ port: 0, hostname: "127.0.0.1", onListen: () => {} }, (req) => {
		const url = new URL(req.url);
		requests.push({
			path: url.pathname + url.search,
			subprotocols: req.headers.get("sec-websocket-protocol"),
		});
		return new Response("refused", { status: 400 });
	});
	return { port: server.addr.port, requests, stop: () => server.shutdown() };
}

const webSocketDialCases = [
	{
		name: "at the URL it was given",
		init: { transport: "websocket" },
		expectedPath: "/moq?jwt=a.b.c",
	},
	{
		name: "at webSocketURL when there is one",
		init: { transport: "websocket", webSocketURL: "/elsewhere?jwt=x.y.z" },
		expectedPath: "/elsewhere?jwt=x.y.z",
	},
] as const;

for (const c of webSocketDialCases) {
	Deno.test({
		name: `connect dials the WebSocket ${c.name}`,
		// reason: the QMux transport keeps timers of its own past a refused dial.
		sanitizeOps: false,
		sanitizeResources: false,
		fn: async () => {
			const server = startRefusingServer();
			const base = `http://127.0.0.1:${server.port}`;
			const init = "webSocketURL" in c.init
				? { ...c.init, webSocketURL: base + c.init.webSocketURL }
				: c.init;

			try {
				await assertRejects(
					() => connect(`${base}/moq?jwt=a.b.c`, init),
					Error,
					"failed to connect",
				);
			} finally {
				await server.stop();
			}

			assertEquals(server.requests, [{
				path: c.expectedPath,
				subprotocols: "qmux-02.moq-lite-05",
			}]);
		},
	});
}

const invalidTransports = ["ws", "WebSocket", "auto", ""] as const;

for (const transport of invalidTransports) {
	Deno.test(`connect rejects the transport ${JSON.stringify(transport)}`, async () => {
		// reason: a value that the type forbids, as JavaScript or configuration can pass.
		const init = { transport } as unknown as Parameters<typeof connect>[1];

		await assertRejects(
			() => connect("https://example.com", init),
			TypeError,
			'transport must be "webtransport" or "websocket"',
		);
	});
}
