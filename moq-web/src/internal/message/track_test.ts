import { assertEquals } from "@std/assert";
import { TrackMessage } from "./track.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("TrackMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"valid message": { broadcastPath: "/live/alice", trackName: "video" },
		"empty fields": { broadcastPath: "", trackName: "" },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(128);
			const message = new TrackMessage(input);
			assertEquals(await message.encode(buffer), undefined);

			const decoded = new TrackMessage({});
			assertEquals(await decoded.decode(buffer), undefined);
			assertEquals(decoded.broadcastPath, input.broadcastPath);
			assertEquals(decoded.trackName, input.trackName);
		});
	}

	await t.step("decode returns error on empty input", async () => {
		const buffer = Buffer.make(0);
		const decoded = new TrackMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
