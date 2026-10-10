import { assertEquals, assertRejects, assertStrictEquals } from "@std/assert";
import { FakeTime } from "@std/testing/time";
import { openFirstReady, TransportTimeoutError } from "./open_transport.ts";

/**
 * A transport whose readiness is set by a field: it becomes ready, fails
 * with `error`, or never settles. It records whether it was closed.
 */
class FakeTransport {
	readonly ready: Promise<undefined>;
	readonly closed: Promise<WebTransportCloseInfo> = new Promise(() => {});
	readonly incomingBidirectionalStreams = new ReadableStream<WebTransportBidirectionalStream>();
	readonly incomingUnidirectionalStreams = new ReadableStream<ReadableStream<Uint8Array>>();
	wasClosed = false;

	constructor(outcome: "ready" | "pending" | Error) {
		if (outcome === "ready") {
			this.ready = Promise.resolve(undefined);
		} else if (outcome === "pending") {
			this.ready = new Promise(() => {});
		} else {
			this.ready = Promise.reject(outcome);
			// reason: the rejection is observed by the code under test, later.
			this.ready.catch(() => {});
		}
	}

	close(): void {
		this.wasClosed = true;
	}

	/** The fake as the `WebTransport` it stands for. */
	asWebTransport(): WebTransport {
		// reason: a structural fake of the members the code under test uses.
		return this as unknown as WebTransport;
	}
}

Deno.test("openFirstReady returns the first transport when it becomes ready", async () => {
	const first = new FakeTransport("ready");
	const second = new FakeTransport("ready");
	let secondOpened = false;

	const got = await openFirstReady([
		() => first.asWebTransport(),
		() => {
			secondOpened = true;
			return second.asWebTransport();
		},
	], 1000);

	await got.ready;
	assertEquals(first.wasClosed, false);
	assertEquals(secondOpened, false);
});

Deno.test("openFirstReady falls back when the first transport fails, and closes it", async () => {
	const first = new FakeTransport(new Error("connection refused"));
	const second = new FakeTransport("ready");

	await openFirstReady([() => first.asWebTransport(), () => second.asWebTransport()], 1000);

	assertEquals(first.wasClosed, true);
	assertEquals(second.wasClosed, false);
});

Deno.test("openFirstReady falls back when opening the first transport throws", async () => {
	const second = new FakeTransport("ready");

	await openFirstReady([
		() => {
			throw new TypeError("bad URL");
		},
		() => second.asWebTransport(),
	], 1000);

	assertEquals(second.wasClosed, false);
});

Deno.test("openFirstReady gives up on a silent first transport after the timeout", async () => {
	using time = new FakeTime();
	const first = new FakeTransport("pending");
	const second = new FakeTransport("ready");

	const opening = openFirstReady(
		[() => first.asWebTransport(), () => second.asWebTransport()],
		5000,
	);
	await time.tickAsync(5000);
	await opening;

	assertEquals(first.wasClosed, true);
	assertEquals(second.wasClosed, false);
});

Deno.test("openFirstReady waits for the last transport without a timeout", async () => {
	using time = new FakeTime();
	const only = new FakeTransport("pending");
	let settled = false;

	const opening = openFirstReady([() => only.asWebTransport()], 5000);
	opening.then(() => settled = true, () => settled = true);
	await time.tickAsync(60_000);

	assertEquals(settled, false);
	assertEquals(only.wasClosed, false);
});

Deno.test("openFirstReady rejects with the last transport's failure", async () => {
	const lastFailure = new Error("handshake failed");
	const first = new FakeTransport(new Error("connection refused"));
	const second = new FakeTransport(lastFailure);

	const err = await assertRejects(() =>
		openFirstReady([() => first.asWebTransport(), () => second.asWebTransport()], 1000)
	);

	assertStrictEquals(err, lastFailure);
	assertEquals(second.wasClosed, true);
});

Deno.test("openFirstReady rejects when there is nothing to open", async () => {
	await assertRejects(() => openFirstReady([], 1000), Error, "no transport to open");
});

Deno.test("TransportTimeoutError names the time waited", () => {
	const err = new TransportTimeoutError(5000);

	assertEquals(err.name, "TransportTimeoutError");
	assertEquals(err.message, "transport not ready after 5000 ms");
});
