import { assertEquals } from "@std/assert";
import { SubscribeOkMessage } from "./subscribe_ok.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("SubscribeOkMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"zero group": { group: 0 },
		"normal case": { group: 5 },
		"large group": { group: 1 << 28 },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(100);
			const message = new SubscribeOkMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new SubscribeOkMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMessage.group,
				input.group,
				`group mismatch for ${caseName}`,
			);
		});
	}

	await t.step("decode should return error when readVarint fails", async () => {
		const buffer = Buffer.make(0); // Empty buffer causes read error
		const message = new SubscribeOkMessage({});
		const err = await message.decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
