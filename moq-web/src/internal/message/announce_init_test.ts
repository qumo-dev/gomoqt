import { assertEquals } from "@std/assert";
import { AnnounceInitMessage } from "./announce_init.ts";
import { Buffer } from "@okdaichi/golikejs/bytes";
import type { Reader, Writer } from "@okdaichi/golikejs/io";

Deno.test("AnnounceInitMessage", async (t) => {
	await t.step("should encode and decode with suffixes", async () => {
		const suffixes = ["suffix1", "suffix2", "suffix3"];

		// Encode using Buffer
		const buffer = Buffer.make(100);
		const message = new AnnounceInitMessage({ suffixes });
		const encodeErr = await message.encode(buffer);
		assertEquals(encodeErr, undefined);

		// Decode from a new buffer with written data
		const readBuffer = Buffer.make(100);
		await readBuffer.write(buffer.bytes());
		const decodedMessage = new AnnounceInitMessage({});
		const decodeErr = await decodedMessage.decode(readBuffer);
		assertEquals(decodeErr, undefined);
		assertEquals(decodedMessage.suffixes, suffixes);
	});

	await t.step("should encode and decode without suffixes", async () => {
		// Encode using Buffer
		const buffer = Buffer.make(100);
		const message = new AnnounceInitMessage({});
		const encodeErr = await message.encode(buffer);
		assertEquals(encodeErr, undefined);

		// Decode from a new buffer with written data
		const readBuffer = Buffer.make(100);
		await readBuffer.write(buffer.bytes());
		const decodedMessage = new AnnounceInitMessage({});
		const decodeErr = await decodedMessage.decode(readBuffer);
		assertEquals(decodeErr, undefined);
		assertEquals(decodedMessage.suffixes, []);
	});

	await t.step(
		"decode should return error when readVarint fails for message length",
		async () => {
			const mockReader: Reader = {
				async read(_p: Uint8Array): Promise<[number, Error | undefined]> {
					return [0, new Error("Read failed")];
				},
			};

			const message = new AnnounceInitMessage({});
			const err = await message.decode(mockReader);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"decode should return error when reading suffix count fails",
		async () => {
			const buffer = Buffer.make(10);
			await buffer.write(new Uint8Array([2])); // only message length

			const message = new AnnounceInitMessage({});
			const err = await message.decode(buffer);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"decode should return error when reading suffix value fails",
		async () => {
			const buffer = Buffer.make(10);
			await buffer.write(new Uint8Array([3, 2])); // message length, suffixCount=2, but no suffix data

			const message = new AnnounceInitMessage({});
			const err = await message.decode(buffer);
			assertEquals(err !== undefined, true);
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

			const message = new AnnounceInitMessage({ suffixes: ["test"] });
			const err = await message.encode(mockWriter);
			assertEquals(err instanceof Error, true);
		},
	);
});
