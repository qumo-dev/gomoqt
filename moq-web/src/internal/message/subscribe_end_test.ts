import { assertEquals } from "@std/assert";
import { SubscribeEndMessage } from "./subscribe_end.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("SubscribeEndMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"zero group (track ended with no groups)": { group: 0 },
		"last group": { group: 42 },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(64);
			const message = new SubscribeEndMessage(input);
			assertEquals(await message.encode(buffer), undefined);

			const decoded = new SubscribeEndMessage({});
			assertEquals(await decoded.decode(buffer), undefined);
			assertEquals(decoded.group, input.group);
		});
	}

	await t.step("decode returns error on empty input", async () => {
		const buffer = Buffer.make(0);
		const decoded = new SubscribeEndMessage({});
		const err = await decoded.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
