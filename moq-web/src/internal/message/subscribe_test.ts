import { assert, assertEquals } from "@std/assert";
import { SubscribeMessage } from "./subscribe.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Writer } from "@okdaichi/golikejs/io";

Deno.test("SubscribeMessage - encode/decode roundtrip - multiple scenarios", async (t) => {
	const testCases = {
		"normal case": {
			subscribeId: 123,
			broadcastPath: "path",
			trackName: "track",
			subscriberPriority: 1,
			subscriberOrdered: 1,
			subscriberMaxLatency: 100,
			startGroup: 5,
			endGroup: 10,
		},
		"large values": {
			subscribeId: 1000000,
			broadcastPath: "long/path/to/resource",
			trackName: "long-track-name-with-hyphens",
			subscriberPriority: 255,
			subscriberOrdered: 0,
			subscriberMaxLatency: 0,
			startGroup: 0,
			endGroup: 0,
		},
		"zero values": {
			subscribeId: 0,
			broadcastPath: "",
			trackName: "",
			subscriberPriority: 0,
			subscriberOrdered: 0,
			subscriberMaxLatency: 0,
			startGroup: 0,
			endGroup: 0,
		},
		"single character paths": {
			subscribeId: 1,
			broadcastPath: "a",
			trackName: "b",
			subscriberPriority: 1,
			subscriberOrdered: 1,
			subscriberMaxLatency: 500,
			startGroup: 0,
			endGroup: 20,
		},
	};

	for (const [caseName, input] of Object.entries(testCases)) {
		await t.step(caseName, async () => {
			// Encode using Buffer
			const buffer = Buffer.make(200);
			const message = new SubscribeMessage(input);
			const encodeErr = await message.encode(buffer);
			assertEquals(encodeErr, undefined, `encode failed for ${caseName}`);

			// Decode from a new buffer with written data
			const readBuffer = Buffer.make(200);
			await readBuffer.write(buffer.bytes());
			const decodedMessage = new SubscribeMessage({});
			const decodeErr = await decodedMessage.decode(readBuffer);
			assertEquals(decodeErr, undefined, `decode failed for ${caseName}`);

			// Verify all fields match
			assertEquals(
				decodedMessage.subscribeId,
				input.subscribeId,
				`subscribeId mismatch for ${caseName}`,
			);
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
				decodedMessage.subscriberPriority,
				input.subscriberPriority,
				`subscriberPriority mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.subscriberOrdered,
				input.subscriberOrdered,
				`subscriberOrdered mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.subscriberMaxLatency,
				input.subscriberMaxLatency,
				`subscriberMaxLatency mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.startGroup,
				input.startGroup,
				`startGroup mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.endGroup,
				input.endGroup,
				`endGroup mismatch for ${caseName}`,
			);
		});
	}

	await t.step(
		"decode should return error when readVarint fails for message length",
		async () => {
			const buffer = Buffer.make(0); // Empty buffer
			const message = new SubscribeMessage({});
			const err = await message.decode(buffer);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"decode should return error when reading subscribeId fails",
		async () => {
			const buffer = Buffer.make(10);
			// message length = 5 (varint), but no data
			await buffer.write(new Uint8Array([0x05]));
			const message = new SubscribeMessage({});
			const err = await message.decode(buffer);
			assert(err !== undefined);
		},
	);

	await t.step(
		"decode should return error when reading broadcastPath fails",
		async () => {
			const buffer = Buffer.make(10);
			// message length = 5 (varint), subscribeId = 1 (varint), but no broadcastPath
			await buffer.write(new Uint8Array([0x05, 0x01]));
			const message = new SubscribeMessage({});
			const err = await message.decode(buffer);
			assert(err !== undefined);
		},
	);

	await t.step(
		"decode should return error when reading trackName fails",
		async () => {
			const buffer = Buffer.make(10);
			// message length = 6 (varint), subscribeId = 1 (varint), empty broadcastPath = 0 (varint), but no trackName
			await buffer.write(new Uint8Array([0x06, 0x01, 0x00]));
			const message = new SubscribeMessage({});
			const err = await message.decode(buffer);
			assert(err !== undefined);
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

			const message = new SubscribeMessage({
				subscribeId: 1,
				broadcastPath: "path",
				trackName: "track",
				subscriberPriority: 1,
			});
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
