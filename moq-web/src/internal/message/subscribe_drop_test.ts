import { assertEquals } from "@std/assert";
import { SubscribeDropMessage } from "./subscribe_drop.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("SubscribeDropMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": { startGroup: 10, endGroup: 20, errorCode: 1 },
		"zero values": { startGroup: 0, endGroup: 0, errorCode: 0 },
		"large numbers": { startGroup: 1000000, endGroup: 2000000, errorCode: 65535 },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(100);
			const msg = new SubscribeDropMessage(input);
			const encodeErr = await msg.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decoded = new SubscribeDropMessage({});
			const decodeErr = await decoded.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(decoded.startGroup, input.startGroup, `startGroup ${caseName}`);
			assertEquals(decoded.endGroup, input.endGroup, `endGroup ${caseName}`);
			assertEquals(decoded.errorCode, input.errorCode, `errorCode ${caseName}`);
		});
	}
});

Deno.test("SubscribeDropMessage - error cases", async (t) => {
	await t.step("encode returns error when the write fails", async () => {
		const writer: Writer = {
			write: async (_p: Uint8Array): Promise<[number, Error | undefined]> => [
				0,
				new Error("Write failed"),
			],
		};
		const err = await new SubscribeDropMessage({ startGroup: 1 }).encode(writer);
		assertEquals(err instanceof Error, true);
	});

	await t.step("decode returns error when readVarint fails for message length", async () => {
		const buffer = Buffer.make(0); // empty: no length prefix
		const err = await new SubscribeDropMessage({}).decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
