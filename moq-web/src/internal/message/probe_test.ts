import { assertEquals } from "@std/assert";
import { ProbeMessage } from "./probe.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("ProbeMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": { bitrate: 5000, rtt: 25 },
		"zero values": { bitrate: 0, rtt: 0 },
		"large numbers": { bitrate: 1000000, rtt: 100000 },
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(100);
			const msg = new ProbeMessage(input);
			const encodeErr = await msg.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decoded = new ProbeMessage({});
			const decodeErr = await decoded.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(decoded.bitrate, input.bitrate, `bitrate ${caseName}`);
			assertEquals(decoded.rtt, input.rtt, `rtt ${caseName}`);
		});
	}
});

Deno.test("ProbeMessage - error cases", async (t) => {
	await t.step("encode returns error when the write fails", async () => {
		const writer: Writer = {
			write: async (_p: Uint8Array): Promise<[number, Error | undefined]> => [
				0,
				new Error("Write failed"),
			],
		};
		const err = await new ProbeMessage({ bitrate: 1 }).encode(writer);
		assertEquals(err instanceof Error, true);
	});

	await t.step("decode returns error when readVarint fails for message length", async () => {
		const buffer = Buffer.make(0); // empty: no length prefix
		const err = await new ProbeMessage({}).decode(buffer);
		assertEquals(err !== undefined, true);
	});
});
