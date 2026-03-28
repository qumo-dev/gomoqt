import { assertEquals, assertRejects } from "@std/assert";
import { Broadcast, NotFound, NotFoundTrackHandler } from "./broadcast.ts";
import type { TrackHandler } from "./track_mux.ts";
import type { TrackWriter } from "./track_writer.ts";
import { SubscribeErrorCode } from "./error.ts";

class MockTrackHandler implements TrackHandler {
	calls: TrackWriter[] = [];
	waitForClose = false;

	async serveTrack(trackWriter: TrackWriter): Promise<void> {
		this.calls.push(trackWriter);
		if (this.waitForClose) {
			await trackWriter.context.done();
		}
	}
}

function newMockTrackWriter(trackName: string) {
	let closeResolve: (() => void) | undefined;
	const closePromise = new Promise<void>((resolve) => {
		closeResolve = resolve;
	});
	const closeWithErrorCalls: number[] = [];
	let closeCalls = 0;
	const trackWriter = {
		trackName,
		context: {
			done: () => closePromise,
		},
		async closeWithError(code: number): Promise<void> {
			closeWithErrorCalls.push(code);
			closeCalls++;
			closeResolve?.();
		},
		async close(): Promise<void> {
			closeCalls++;
			closeResolve?.();
		},
	} as TrackWriter & {
		closeWithErrorCalls: number[];
		closeCalls: number;
	};
	trackWriter.closeWithErrorCalls = closeWithErrorCalls;
	trackWriter.closeCalls = closeCalls;
	return {
		trackWriter,
		get closeCalls() {
			return closeCalls;
		},
		get closeWithErrorCalls() {
			return closeWithErrorCalls;
		},
	};
}

Deno.test("Broadcast register and serveTrack routes to registered handler", async () => {
	const broadcast = new Broadcast();
	const handler = new MockTrackHandler();
	await broadcast.register("video", handler);

	const mock = newMockTrackWriter("video");
	await broadcast.serveTrack(mock.trackWriter);

	assertEquals(handler.calls.length, 1);
	assertEquals(handler.calls[0], mock.trackWriter);
});

Deno.test("Broadcast handler returns NotFoundTrackHandler for empty name", () => {
	const broadcast = new Broadcast();
	assertEquals(broadcast.handler(""), NotFoundTrackHandler);
});

Deno.test("Broadcast remove returns false when track is missing", async () => {
	const broadcast = new Broadcast();
	assertEquals(await broadcast.remove("missing"), false);
	assertEquals(await broadcast.remove(""), false);
});

Deno.test("Broadcast register rejects invalid input", async () => {
	const broadcast = new Broadcast();
	await assertRejects(
		() => broadcast.register("", new MockTrackHandler()),
		Error,
		"track name is required",
	);
	await assertRejects(
		() => broadcast.register("video", undefined as unknown as TrackHandler),
		Error,
		"track handler cannot be nil",
	);
});

Deno.test("Broadcast remove closes active tracks", async () => {
	const broadcast = new Broadcast();
	const handler = new MockTrackHandler();
	handler.waitForClose = true;
	await broadcast.register("video", handler);

	const mock = newMockTrackWriter("video");
	const serving = broadcast.serveTrack(mock.trackWriter);
	await new Promise((resolve) => setTimeout(resolve, 0));

	const removed = await broadcast.remove("video");
	await serving;

	assertEquals(removed, true);
	assertEquals(mock.closeCalls, 1);
});

Deno.test("Broadcast replacement closes previous active tracks", async () => {
	const broadcast = new Broadcast();
	const first = new MockTrackHandler();
	first.waitForClose = true;
	await broadcast.register("video", first);

	const oldTrack = newMockTrackWriter("video");
	const oldServing = broadcast.serveTrack(oldTrack.trackWriter);
	await new Promise((resolve) => setTimeout(resolve, 0));

	const second = new MockTrackHandler();
	await broadcast.register("video", second);
	await oldServing;

	const newTrack = newMockTrackWriter("video");
	await broadcast.serveTrack(newTrack.trackWriter);

	assertEquals(oldTrack.closeCalls, 1);
	assertEquals(second.calls.length, 1);
	assertEquals(second.calls[0], newTrack.trackWriter);
});

Deno.test("Broadcast close closes all active tracks", async () => {
	const broadcast = new Broadcast();
	const handler = new MockTrackHandler();
	handler.waitForClose = true;
	await broadcast.register("video", handler);
	await broadcast.register("audio", handler);

	const first = newMockTrackWriter("video");
	const second = newMockTrackWriter("audio");
	const serve1 = broadcast.serveTrack(first.trackWriter);
	const serve2 = broadcast.serveTrack(second.trackWriter);
	await new Promise((resolve) => setTimeout(resolve, 0));

	await broadcast.close();
	await Promise.all([serve1, serve2]);

	assertEquals(first.closeCalls, 1);
	assertEquals(second.closeCalls, 1);
});

Deno.test("NotFound and NotFoundTrackHandler close with track-not-found", async () => {
	const first = newMockTrackWriter("missing");
	await NotFound(first.trackWriter);
	assertEquals(first.closeWithErrorCalls, [SubscribeErrorCode.TrackNotFound]);

	const second = newMockTrackWriter("missing");
	await NotFoundTrackHandler.serveTrack(second.trackWriter);
	assertEquals(second.closeWithErrorCalls, [SubscribeErrorCode.TrackNotFound]);
});
