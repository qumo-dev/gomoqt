import { assertEquals } from "@std/assert";
import { GroupMessage } from "./group.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Reader, Writer } from "@okdaichi/golikejs/io";

Deno.test("GroupMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": {
			subscribeId: 123,
			sequence: 456,
		},
		"zero values": {
			subscribeId: 0,
			sequence: 0,
		},
		"large numbers": {
			subscribeId: 1000000,
			sequence: 2000000,
		},
		"single values": {
			subscribeId: 1,
			sequence: 1,
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			// Encode using Buffer
			const buffer = Buffer.make(100);
			const msg = new GroupMessage(input);
			const encodeErr = await msg.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			// Decode from a new buffer with written data
			const readBuffer = Buffer.make(100);
			await readBuffer.write(buffer.bytes());
			const decodedMsg = new GroupMessage({});
			const decodeErr = await decodedMsg.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMsg.subscribeId,
				input.subscribeId,
				`subscribeId mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMsg.sequence,
				input.sequence,
				`sequence mismatch for ${caseName}`,
			);
		});
	}
});

Deno.test("GroupMessage - error cases", async (t) => {
	await t.step(
		"decode should return error when readVarint fails for message length",
		async () => {
			const mockReader: Reader = {
				async read(_p: Uint8Array): Promise<[number, Error | undefined]> {
					return [0, new Error("Read failed")];
				},
			};

			const msg = new GroupMessage({});
			const err = await msg.decode(mockReader);
			if (!(err !== undefined)) throw new Error("expected error from decode");
		},
	);

	await t.step(
		"decode should return error when reading subscribeId fails",
		async () => {
			// message length only, no subscribeId
			const buffer = Buffer.make(10);
			await buffer.write(new Uint8Array([1]));

			const msg = new GroupMessage({});
			const err = await msg.decode(buffer);
			if (!(err !== undefined)) {
				throw new Error(
					"expected error from decode subscribeId",
				);
			}
		},
	);

	await t.step(
		"decode should return error when reading sequence fails",
		async () => {
			// Provide length and subscribeId but no sequence
			const buffer = Buffer.make(10);
			await buffer.write(new Uint8Array([2, 1]));

			const msg = new GroupMessage({});
			const err = await msg.decode(buffer);
			if (!(err !== undefined)) {
				throw new Error(
					"expected error from decode sequence",
				);
			}
		},
	);

	await t.step(
		"encode should return error when writeUint16 fails",
		async () => {
			const mockWriter: Writer = {
				async write(_p: Uint8Array): Promise<[number, Error | undefined]> {
					return [0, new Error("Write failed")];
				},
			};

			const msg = new GroupMessage({
				subscribeId: 1,
				sequence: 1,
			});
			const err = await msg.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
