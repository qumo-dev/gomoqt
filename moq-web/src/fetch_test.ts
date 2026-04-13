import { assertEquals, assertExists } from "@std/assert";
import { FetchRequest } from "./fetch.ts";

Deno.test("FetchRequest", async (t) => {
	await t.step("constructor sets all fields from init", () => {
		const req = new FetchRequest({
			broadcastPath: "/live/stream",
			trackName: "video",
			priority: 5,
			groupSequence: 42,
		});
		assertEquals(req.broadcastPath, "/live/stream");
		assertEquals(req.trackName, "video");
		assertEquals(req.priority, 5);
		assertEquals(req.groupSequence, 42);
	});

	await t.step("done returns a pending promise by default", () => {
		const req = new FetchRequest({
			broadcastPath: "/path",
			trackName: "track",
			priority: 0,
			groupSequence: 0,
		});
		const d = req.done();
		assertExists(d);
		// The default done promise should never resolve (pending forever)
	});

	await t.step("done returns the provided promise", async () => {
		let resolve!: () => void;
		const done = new Promise<void>((r) => {
			resolve = r;
		});
		const req = new FetchRequest({
			broadcastPath: "/path",
			trackName: "track",
			priority: 0,
			groupSequence: 0,
			done,
		});
		resolve();
		await req.done();
	});

	await t.step("withDone returns a new FetchRequest with new done promise", async () => {
		const original = new FetchRequest({
			broadcastPath: "/live",
			trackName: "audio",
			priority: 3,
			groupSequence: 7,
		});

		let resolve!: () => void;
		const newDone = new Promise<void>((r) => {
			resolve = r;
		});

		const updated = original.withDone(newDone);

		// Fields should be copied
		assertEquals(updated.broadcastPath, "/live");
		assertEquals(updated.trackName, "audio");
		assertEquals(updated.priority, 3);
		assertEquals(updated.groupSequence, 7);

		// done should be the new promise
		resolve();
		await updated.done();
	});

	await t.step("clone returns a new FetchRequest with new done promise", async () => {
		const original = new FetchRequest({
			broadcastPath: "/clone/test",
			trackName: "data",
			priority: 10,
			groupSequence: 99,
		});

		let resolve!: () => void;
		const cloneDone = new Promise<void>((r) => {
			resolve = r;
		});

		const cloned = original.clone(cloneDone);

		assertEquals(cloned.broadcastPath, "/clone/test");
		assertEquals(cloned.trackName, "data");
		assertEquals(cloned.priority, 10);
		assertEquals(cloned.groupSequence, 99);

		resolve();
		await cloned.done();
	});
});
