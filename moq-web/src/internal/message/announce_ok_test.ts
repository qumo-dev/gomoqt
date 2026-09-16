import { assertEquals } from "@std/assert";
import { AnnounceOkMessage } from "./announce_ok.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("AnnounceOkMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"zero values": { hopID: 0, activeCount: 0 },
		"non-relay with initial set": { hopID: 0, activeCount: 3 },
		"relay with hop id": { hopID: 12345, activeCount: 128 },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(64);
			const message = new AnnounceOkMessage(input);
			assertEquals(await message.encode(buffer), undefined);

			const decoded = new AnnounceOkMessage({});
			assertEquals(await decoded.decode(buffer), undefined);
			assertEquals(decoded.hopID, input.hopID);
			assertEquals(decoded.activeCount, input.activeCount);
		});
	}

	await t.step("decode returns error on empty input", async () => {
		const buffer = Buffer.make(0);
		const decoded = new AnnounceOkMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
