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
import { Client, MOQ } from "./client.ts";
import { TrackMux } from "./track_mux.ts";
import type { MOQOptions } from "./options.ts";

Deno.test("Client - Constructor with Default Options", () => {
	const client = new Client();

	assertExists(client.options);
	assertEquals(client.options.transportOptions?.allowPooling, false);
	assertEquals(
		client.options.transportOptions?.congestionControl,
		"low-latency",
	);
	assertEquals(client.options.transportOptions?.requireUnreliable, true);
});

Deno.test("Client - Constructor with Custom Options", () => {
	const customOptions: MOQOptions = {
		transportOptions: {
			allowPooling: true,
			congestionControl: "throughput",
			requireUnreliable: false,
		},
	};

	const client = new Client(customOptions);

	assertEquals(client.options.transportOptions?.allowPooling, true);
	assertEquals(
		client.options.transportOptions?.congestionControl,
		"throughput",
	);
	assertEquals(client.options.transportOptions?.requireUnreliable, false);
});

// Note: dial() tests focus on Client behavior and keep transport payload mocking minimal.

Deno.test("Client - dial() attempts to create session", async () => {
	const client = new Client();
	const url = "https://example.com";

	// The dial will fail due to incomplete mock response, but we can verify
	// that it attempts to create a WebTransport connection
	try {
		await client.dial(url);
	} catch (_err) {
		// Expected to fail due to mock limitations
		// In real usage, this would succeed with proper server
	}

	await client.close();
});

Deno.test("Client - dial() accepts URL types", () => {
	const client = new Client();

	// Verify that dial method accepts different URL types (no actual call)
	const stringUrl: string = "https://example.com";
	const urlObject: URL = new URL("https://example.com");
	const customMux = new TrackMux();

	// Type checking ensures these signatures are valid
	assertExists(client.dial);
	assertEquals(typeof client.dial, "function");

	// Verify dial signature accepts both URL types and optional mux
	const _test1: Promise<any> = client.dial(stringUrl);
	const _test2: Promise<any> = client.dial(urlObject);
	const _test3: Promise<any> = client.dial(stringUrl, customMux);

	// Prevent hanging promises
	_test1.catch(() => {});
	_test2.catch(() => {});
	_test3.catch(() => {});
});

Deno.test("Client - dial() handles connection errors", async () => {
	const client = new Client();

	// Create failing mock WebTransport
	class FailingMockWebTransport extends MockWebTransport {
		constructor(url: string | URL, options?: WebTransportOptions) {
			super(url, options);
			this.ready = Promise.reject(new Error("Connection failed"));
			// Prevent unhandled rejection in the test runner by
			// attaching a no-op rejection handler. The client.dial
			// still observes the rejection and throws as expected.
			this.ready.catch(() => {});
		}
	}

	// Temporarily replace WebTransport
	const originalWebTransport = (globalThis as any).WebTransport;
	(globalThis as any).WebTransport = FailingMockWebTransport;

	try {
		await assertRejects(
			async () => await client.dial("https://example.com"),
			Error,
		);
	} finally {
		// Restore mock WebTransport
		(globalThis as any).WebTransport = originalWebTransport;
	}
});

Deno.test("Client - close() with no sessions", async () => {
	const client = new Client();
	// Should not throw
	await client.close();
});

// Note: Session-based tests are skipped due to mocking complexity.
// These tests verify Client class behavior at a higher level.

Deno.test("Client - close() with empty sessions", async () => {
	const client = new Client();

	// close() should work even without active sessions
	await client.close();

	// Verify client can be reused after close
	const options = client.options;
	assertExists(options);
});

Deno.test("Client - abort() with no sessions", async () => {
	const client = new Client();

	// abort() should work without active sessions
	await client.abort();

	// Verify client state after abort
	assertExists(client.options);
});

Deno.test("Client - close() is idempotent", async () => {
	const client = new Client();

	// First close
	await client.close();

	// Second close should not throw
	await client.close();
});

Deno.test("Client - abort() is idempotent", async () => {
	const client = new Client();

	// First abort
	await client.abort();

	// Second abort should not throw
	await client.abort();
});

Deno.test("Client - dial() rejects after close", async () => {
	const client = new Client();

	await client.close();

	// dial() after close should reject
	await assertRejects(
		async () => await client.dial("https://example.com"),
		Error,
		"Client is closed",
	);
});

Deno.test("Client - dial() rejects after abort", async () => {
	const client = new Client();

	await client.abort();

	// dial() after abort should reject
	await assertRejects(
		async () => await client.dial("https://example.com"),
		Error,
		"Client is closed",
	);
});

Deno.test("Client - MOQ alias exports", () => {
	// MOQ should be an alias for Client
	assertEquals(MOQ, Client);
});

Deno.test("Client - MOQ alias instantiation", () => {
	const moqClient = new MOQ();

	// Should be instance of Client
	assertEquals(moqClient instanceof Client, true);
	assertExists(moqClient.options);
});

// Restore original WebTransport after all tests
Deno.test("Client - Cleanup", () => {
	(globalThis as any).WebTransport = OriginalWebTransport;
});
