import { assert, assertEquals } from "@std/assert";
import { AnnounceMessage } from "./announce.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("AnnounceMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case with active true": {
			suffix: "test",
			active: true,
			hopIDs: [1, 2, 3],
		},
		"normal case with active false": {
			suffix: "test",
			active: false,
			hopIDs: [],
		},
		"empty suffix": {
			suffix: "",
			active: true,
			hopIDs: [42],
		},
		"long suffix": {
			suffix: "very/long/path/to/broadcast/suffix",
			active: true,
			hopIDs: [],
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			// Encode using Buffer
			const buffer = Buffer.make(100);
			const message = new AnnounceMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			// Decode from a new buffer with written data
			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new AnnounceMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMessage.suffix,
				input.suffix,
				`suffix mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.active,
				input.active,
				`active mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.hopIDs,
				input.hopIDs,
				`hopIDs mismatch for ${caseName}`,
			);
		});
	}

	await t.step(
		"decode should return error when readVarint fails for message length",
		async () => {
			const buffer = Buffer.make(0); // Empty buffer
			const message = new AnnounceMessage({});
			const err = await message.decode(buffer);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"decode should return error when readFull fails",
		async () => {
			const buffer = Buffer.make(10);
			// Write message length = 10 as varint (0x0a), but no data follows
			await buffer.write(new Uint8Array([0x0a]));
			const message = new AnnounceMessage({});
			const err = await message.decode(buffer);
			assert(err !== undefined);
		},
	);

	// Encode error tests
	await t.step(
		"encode should return error when writeUint16 fails",
		async () => {
			const mockWriter: Writer = {
				async write(_p: Uint8Array): Promise<[number, Error | undefined]> {
					return [0, new Error("Write failed")];
				},
			};

			const message = new AnnounceMessage({ suffix: "test", active: true });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"encode should return error when writeVarint fails for status",
		async () => {
			let callCount = 0;
			const mockWriter: Writer = {
				async write(p: Uint8Array): Promise<[number, Error | undefined]> {
					callCount++;
					if (callCount > 1) {
						return [0, new Error("Write failed")];
					}
					return [p.length, undefined];
				},
			};

			const message = new AnnounceMessage({ suffix: "test", active: true });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);

	await t.step(
		"encode should return error when writeString fails for suffix",
		async () => {
			let callCount = 0;
			const mockWriter: Writer = {
				async write(p: Uint8Array): Promise<[number, Error | undefined]> {
					callCount++;
					if (callCount > 2) {
						return [0, new Error("Write failed")];
					}
					return [p.length, undefined];
				},
			};

			const message = new AnnounceMessage({ suffix: "test", active: true });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
