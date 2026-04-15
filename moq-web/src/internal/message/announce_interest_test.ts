import { assert, assertEquals } from "@std/assert";
import { AnnounceInterestMessage } from "./announce_interest.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("AnnounceInterestMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": {
			prefix: "test",
			excludeHop: 0,
		},
		"empty prefix": {
			prefix: "",
			excludeHop: 0,
		},
		"long prefix": {
			prefix: "very/long/path/to/namespace/prefix",
			excludeHop: 42,
		},
		"prefix with special characters": {
			prefix: "test-prefix_123",
			excludeHop: 7,
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			// Encode using Buffer
			const buffer = Buffer.make(100);
			const message = new AnnounceInterestMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			// Decode from a new buffer with written data
			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new AnnounceInterestMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMessage.prefix,
				input.prefix,
				`prefix mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.excludeHop,
				input.excludeHop,
				`excludeHop mismatch for ${caseName}`,
			);
		});
	}

	await t.step("decode should return error when readVarint fails", async () => {
		const buffer = Buffer.make(0); // Empty buffer
		const message = new AnnounceInterestMessage({});
		const err = await message.decode(buffer);
		assertEquals(err !== undefined, true);
	});

	await t.step("decode should return error when readFull fails", async () => {
		const buffer = Buffer.make(10);
		// Write message length = 10 as varint (0x0a), but no data follows
		await buffer.write(new Uint8Array([0x0a]));
		const message = new AnnounceInterestMessage({});
		const err = await message.decode(buffer);
		assert(err !== undefined);
	});

	await t.step(
		"encode should return error when writeVarint fails",
		async () => {
			let callCount = 0;
			const mockWriter: Writer = {
				async write(_p: Uint8Array): Promise<[number, Error | undefined]> {
					callCount++;
					if (callCount > 0) {
						return [0, new Error("Write failed")];
					}
					return [_p.length, undefined];
				},
			};

			const message = new AnnounceInterestMessage({ prefix: "test" });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"encode should return error when writeString fails",
		async () => {
			let callCount = 0;
			const mockWriter: Writer = {
				async write(p: Uint8Array): Promise<[number, Error | undefined]> {
					callCount++;
					if (callCount > 1) {
						return [0, new Error("Write failed on string")];
					}
					return [p.length, undefined];
				},
			};

			const message = new AnnounceInterestMessage({ prefix: "test" });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
