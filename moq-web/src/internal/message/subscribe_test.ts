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
			subscriberMaxAge: 100,
			groupStart: 5,
			groupEnd: 10,
		},
		"large values": {
			subscribeId: 1000000,
			broadcastPath: "long/path/to/resource",
			trackName: "long-track-name-with-hyphens",
			subscriberPriority: 255,
			subscriberMaxAge: 0,
			groupStart: 0,
			groupEnd: 0,
		},
		"zero values": {
			subscribeId: 0,
			broadcastPath: "",
			trackName: "",
			subscriberPriority: 0,
			subscriberMaxAge: 0,
			groupStart: 0,
			groupEnd: 0,
		},
		"single character paths": {
			subscribeId: 1,
			broadcastPath: "a",
			trackName: "b",
			subscriberPriority: 1,
			subscriberMaxAge: 500,
			groupStart: 0,
			groupEnd: 20,
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
				decodedMessage.subscriberMaxAge,
				input.subscriberMaxAge,
				`subscriberMaxAge mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.groupStart,
				input.groupStart,
				`groupStart mismatch for ${caseName}`,
			);
			assertEquals(
				decodedMessage.groupEnd,
				input.groupEnd,
				`groupEnd mismatch for ${caseName}`,
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

Deno.test("SubscribeMessage - decode rejects a malformed body", async (t) => {
	// A valid SUBSCRIBE body, then variants that cut a varint short or leave
	// bytes over. Each must be an error, never a NaN field.
	const full = new Buffer(new ArrayBuffer(0));
	await new SubscribeMessage({
		subscribeId: 1,
		broadcastPath: "/a",
		trackName: "v",
		subscriberMaxAge: 1500, // a 2-byte varint
	}).encode(full);
	const encoded = full.bytes();
	// encoded[0] is the 1-byte length prefix; the body follows.
	const body = encoded.subarray(1);

	const cases: Record<string, Uint8Array> = {
		"a truncated Max Age varint": body.subarray(0, 9), // id, path, track, priority, first byte of Max Age
		"a body that ends before Frame End": body.subarray(0, body.length - 1),
		"trailing bytes": new Uint8Array([...body, 0x00]),
		"a string longer than the body": new Uint8Array([0x01, 0x3f, 0x2f]),
	};
	for (const [name, b] of Object.entries(cases)) {
		await t.step(name, async () => {
			const buf = new Buffer(new ArrayBuffer(0));
			await buf.write(new Uint8Array([b.length, ...b]));
			const msg = new SubscribeMessage();
			const err = await msg.decode(buf);
			assert(err instanceof Error, `${name}: decode should fail`);
			assert(!Number.isNaN(msg.subscriberMaxAge), `${name}: no NaN field`);
		});
	}
});
