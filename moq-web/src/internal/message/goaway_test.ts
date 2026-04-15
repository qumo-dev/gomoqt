import { assert, assertEquals } from "@std/assert";
import { GoawayMessage } from "./goaway.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("GoawayMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": {
			newSessionURI: "https://example.com/new-session",
		},
		"empty URI": {
			newSessionURI: "",
		},
		"long URI": {
			newSessionURI: "https://very-long-host.example.com/path/to/new/session?token=abc123",
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(200);
			const message = new GoawayMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			const readBuffer = Buffer.make(200);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new GoawayMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMessage.newSessionURI,
				input.newSessionURI,
				`newSessionURI mismatch for ${caseName}`,
			);
		});
	}

	await t.step("decode should return error when readVarint fails", async () => {
		const buffer = Buffer.make(0);
		const message = new GoawayMessage({});
		const err = await message.decode(buffer);
		assertEquals(err !== undefined, true);
	});

	await t.step("decode should return error when readFull fails", async () => {
		const buffer = Buffer.make(10);
		await buffer.write(new Uint8Array([0x0a]));
		const message = new GoawayMessage({});
		const err = await message.decode(buffer);
		assert(err !== undefined);
	});

	await t.step(
		"encode should return error when writeVarint fails",
		async () => {
			const mockWriter: Writer = {
				async write(_p: Uint8Array): Promise<[number, Error | undefined]> {
					return [0, new Error("Write failed")];
				},
			};

			const message = new GoawayMessage({ newSessionURI: "https://example.com" });
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

			const message = new GoawayMessage({ newSessionURI: "https://example.com" });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
