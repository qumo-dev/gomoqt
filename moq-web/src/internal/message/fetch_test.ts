import { assert, assertEquals } from "@std/assert";
import { FetchMessage } from "./fetch.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("FetchMessage - encode/decode roundtrip", async (t) => {
	const testCases = {
		"normal case": {
			broadcastPath: "/live/stream",
			trackName: "video",
			priority: 1,
			groupSequence: 42,
		},
		"zero values": {
			broadcastPath: "",
			trackName: "",
			priority: 0,
			groupSequence: 0,
		},
		"large values": {
			broadcastPath: "long/path/to/resource",
			trackName: "long-track-name-with-hyphens",
			priority: 255,
			groupSequence: 1000000,
		},
		"single character": {
			broadcastPath: "a",
			trackName: "b",
			priority: 10,
			groupSequence: 1,
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			const buffer = Buffer.make(200);
			const message = new FetchMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			const readBuffer = Buffer.make(200);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new FetchMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);
			assertEquals(
				decodedMessage.broadcastPath,
				input.broadcastPath,
				`broadcastPath mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.trackName,
				input.trackName,
				`trackName mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.priority,
				input.priority,
				`priority mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.groupSequence,
				input.groupSequence,
				`groupSequence mismatch for ${caseName}`,
			);
		});
	}

	await t.step(
		"decode should return error when readVarint fails for message length",
		async () => {
			const buffer = Buffer.make(0);
			const message = new FetchMessage({});
			const err = await message.decode(buffer);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step("decode should return error on message length mismatch", async () => {
		const buffer = Buffer.make(20);
		// Write message length = 10, then only fill part of the data
		// The extra bytes after proper decode will trigger mismatch
		const message = new FetchMessage({
			broadcastPath: "a",
			trackName: "b",
			priority: 1,
			groupSequence: 1,
		});
		const encodeErr = await message.encode(buffer);
		assertEquals(encodeErr, undefined);

		// Tamper: prepend a larger message length
		const readBuffer = Buffer.make(20);
		// Write varint 20 (larger than actual body)
		await readBuffer.write(new Uint8Array([20]));
		// Write enough data to fill
		const pad = new Uint8Array(20);
		await readBuffer.write(pad);

		const decoded = new FetchMessage({});
		const decodeErr = await decoded.decode(readBuffer);
		assert(decodeErr !== undefined);
	});
});
