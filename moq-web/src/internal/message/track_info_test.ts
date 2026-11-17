import { assertEquals } from "@std/assert";
import { TrackInfoMessage } from "./track_info.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("TrackInfoMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"millisecond timescale": {
			publisherPriority: 5,
			publisherMaxAge: 2000,
			timescale: 1000,
		},
		"rtp video clock": {
			publisherPriority: 128,
			publisherMaxAge: 0,
			timescale: 90000,
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(64);
			const message = new TrackInfoMessage(input);
			assertEquals(await message.encode(buffer), undefined);

			const decoded = new TrackInfoMessage({});
			assertEquals(await decoded.decode(buffer), undefined);
			assertEquals(decoded.publisherPriority, input.publisherPriority);
			assertEquals(decoded.publisherMaxAge, input.publisherMaxAge);
			assertEquals(decoded.timescale, input.timescale);
		});
	}

	await t.step("decode returns error on empty input", async () => {
		const buffer = Buffer.make(0);
		const decoded = new TrackInfoMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
