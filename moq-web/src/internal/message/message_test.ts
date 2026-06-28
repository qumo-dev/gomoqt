import { assertEquals, assertThrows } from "@std/assert";
import { spy } from "@std/testing/mock";
import {
	MAX_BYTES_LENGTH,
	MessageEncoder,
	parseBytes,
	parseString,
	parseStringArray,
	parseVarint,
	putVarint,
	readBytes,
	readFull,
	readString,
	readStringArray,
	readVarint,
	writeBytes,
	writeString,
	writeStringArray,
	writeVarint,
} from "./message.ts";
import { GroupMessage } from "./group.ts";
import type { Writer } from "@okdaichi/golikejs/io";
import { EOFError } from "@okdaichi/golikejs/io";
import { Buffer } from "@okdaichi/golikejs/bytes";

Deno.test("message utilities", async (t) => {
	await t.step("readFull - reads exactly the requested bytes", async () => {
		const buffer = Buffer.make(10);
		buffer.write(new Uint8Array([1, 2, 3, 4, 5]));

		const result = new Uint8Array(3);
		const [n, err] = await readFull(buffer, result);
		assertEquals(n, 3);
		assertEquals(err, undefined);
		assertEquals(result, new Uint8Array([1, 2, 3]));
	});

	await t.step("readFull - returns EOF when not enough data", async () => {
		const buffer = Buffer.make(10);
		buffer.write(new Uint8Array([1, 2]));

		const result = new Uint8Array(5);
		const [n, err] = await readFull(buffer, result);
		assertEquals(n, 2);
		assertEquals(err?.name, "EOFError");
	});

	await t.step(
		"readFull - returns EOF when reader returns 0 bytes without error",
		async () => {
			// Mock reader that returns 0 bytes on second read
			let callCount = 0;
			const reader = {
				read: async (p: Uint8Array) => {
					callCount++;
					if (callCount === 1) {
						p[0] = 1;
						return [1, undefined] as [number, Error | undefined];
					}
					return [0, undefined] as [number, Error | undefined]; // 0 bytes without error
				},
			};

			const result = new Uint8Array(5);
			const [n, err] = await readFull(reader, result);
			assertEquals(n, 1);
			assertEquals(err?.name, "EOFError");
		},
	);

	await t.step("writeVarint - writes single byte varint", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, 42);
		assertEquals(n, 1);
		assertEquals(err, undefined);
		assertEquals(buffer.bytes(), new Uint8Array([42]));
	});

	await t.step("writeVarint - writes two byte varint", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, 1000);
		assertEquals(n, 2);
		assertEquals(err, undefined);
		assertEquals(buffer.bytes(), new Uint8Array([0x43, 0xe8]));
	});

	await t.step("writeVarint - writes four byte varint", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, 100000);
		assertEquals(n, 4);
		assertEquals(err, undefined);
		assertEquals(buffer.bytes(), new Uint8Array([0x80, 0x01, 0x86, 0xa0]));
	});

	await t.step("writeVarint - writes eight byte varint", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, 4294967296);
		assertEquals(n, 8);
		assertEquals(err, undefined);
	});

	await t.step("writeVarint - rejects negative numbers", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, -1);
		assertEquals(n, 0);
		assertEquals(err?.message, "Varint cannot be negative");
	});

	await t.step("writeVarint - rejects too large numbers", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeVarint(buffer, Infinity);
		assertEquals(n, 0);
		assertEquals(err?.name, "RangeError");
	});

	await t.step("writeBytes - writes bytes with length prefix", async () => {
		const buffer = Buffer.make(10);

		const data = new Uint8Array([1, 2, 3]);
		const [n, err] = await writeBytes(buffer, data);
		assertEquals(n, 4);
		assertEquals(err, undefined);
		assertEquals(buffer.bytes(), new Uint8Array([3, 1, 2, 3])); // length prefix + data
	});

	await t.step("writeString - writes string with length prefix", async () => {
		const buffer = Buffer.make(10);

		const [n, err] = await writeString(buffer, "hello");
		assertEquals(n, 6);
		assertEquals(err, undefined);
		assertEquals(buffer.bytes(), new Uint8Array([5, 104, 101, 108, 108, 111])); // length + "hello"
	});

	await t.step("writeStringArray - writes string array", async () => {
		const buffer = Buffer.make(50);

		const arr = ["hello", "world"];
		const [n, err] = await writeStringArray(buffer, arr);
		assertEquals(err, undefined);
		assertEquals(n > 0, true);

		// Read back to verify
		const readBuffer = Buffer.make(50);
		await readBuffer.write(buffer.bytes());
		const [readArr, , readErr] = await readStringArray(readBuffer);
		assertEquals(readErr, undefined);
		assertEquals(readArr, arr);
	});

	await t.step("readVarint - reads single byte varint", async () => {
		const buffer = Buffer.make(10);
		buffer.write(new Uint8Array([42]));

		const [value, n, err] = await readVarint(buffer);
		assertEquals(value, 42);
		assertEquals(n, 1);
		assertEquals(err, undefined);
	});

	await t.step("readVarint - reads two byte varint", async () => {
		const buffer = Buffer.make(10);
		buffer.write(new Uint8Array([0x43, 0xe8]));

		const [value, n, err] = await readVarint(buffer);
		assertEquals(value, 1000);
		assertEquals(n, 2);
		assertEquals(err, undefined);
	});

	await t.step("readVarint - reads four byte varint", async () => {
		const buffer = Buffer.make(10);
		buffer.write(new Uint8Array([0x80, 0x01, 0x86, 0xa0]));

		const [value, n, err] = await readVarint(buffer);
		assertEquals(value, 100000);
		assertEquals(n, 4);
		assertEquals(err, undefined);
	});

	await t.step("readVarint - reads eight byte varint", async () => {
		const writtenData: Uint8Array[] = [];
		const writerMock = {
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
		};
		await writeVarint(writerMock, 4294967296);
		const allData = new Uint8Array(
			writtenData.reduce((a, b) => a + b.length, 0),
		);
		let off = 0;
		for (const d of writtenData) {
			allData.set(d, off);
			off += d.length;
		}
		let readOffset = 0;
		const reader = {
			id: 0n,
			read: spy(async (p: Uint8Array) => {
				if (readOffset >= allData.length) {
					return [0, new EOFError()] as [number, Error | undefined];
				}
				const n = Math.min(p.length, allData.length - readOffset);
				p.set(allData.subarray(readOffset, readOffset + n));
				readOffset += n;
				return [n, undefined] as [number, Error | undefined];
			}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};

		const [value, n, err] = await readVarint(reader);
		assertEquals(value, 4294967296);
		assertEquals(n, 8);
		assertEquals(err, undefined);
	});

	await t.step("readBytes - reads bytes with length prefix", async () => {
		const buffer = Buffer.make(10);
		const data = new Uint8Array([1, 2, 3]);
		await writeBytes(buffer, data);

		const readBuffer = Buffer.make(10);
		readBuffer.write(buffer.bytes());
		const [result, n, err] = await readBytes(readBuffer);
		assertEquals(result, data);
		assertEquals(n, 4);
		assertEquals(err, undefined);
	});

	await t.step("readBytes - rejects too large length", async () => {
		const writtenData: Uint8Array[] = [];
		const writerMock = {
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
		};
		await writeVarint(writerMock, MAX_BYTES_LENGTH + 1);
		const allData = new Uint8Array(
			writtenData.reduce((a, b) => a + b.length, 0),
		);
		let off = 0;
		for (const d of writtenData) {
			allData.set(d, off);
			off += d.length;
		}
		let readOffset = 0;
		const reader = {
			id: 0n,
			read: spy(async (p: Uint8Array) => {
				if (readOffset >= allData.length) {
					return [0, new EOFError()] as [number, Error | undefined];
				}
				const n = Math.min(p.length, allData.length - readOffset);
				p.set(allData.subarray(readOffset, readOffset + n));
				readOffset += n;
				return [n, undefined] as [number, Error | undefined];
			}),
			cancel: spy(async (_code: number) => {}),
			closed: () => new Promise<void>(() => {}),
		};

		const [result, _n, err] = await readBytes(reader);
		assertEquals(result.length, 0);
		assertEquals(err?.message, "Bytes length exceeds maximum limit");
	});

	await t.step("readString - reads string with length prefix", async () => {
		const buffer = Buffer.make(10);
		await writeString(buffer, "hello");

		const readBuffer = Buffer.make(10);
		readBuffer.write(buffer.bytes());
		const [result, n, err] = await readString(readBuffer);
		assertEquals(result, "hello");
		assertEquals(n, 6);
		assertEquals(err, undefined);
	});

	await t.step("readStringArray - reads string array", async () => {
		const buffer = Buffer.make(50);
		const arr = ["hello", "world"];
		await writeStringArray(buffer, arr);

		const readBuffer = Buffer.make(50);
		readBuffer.write(buffer.bytes());
		const [result, _n, err] = await readStringArray(readBuffer);
		assertEquals(result, arr);
		assertEquals(err, undefined);
	});

	await t.step("readStringArray - rejects too large count", async () => {
		const buffer = Buffer.make(10);
		await writeVarint(buffer, MAX_BYTES_LENGTH + 1);

		const readBuffer = Buffer.make(10);
		readBuffer.write(buffer.bytes());
		const [result, _n2, err] = await readStringArray(readBuffer);
		assertEquals(result.length, 0);
		assertEquals(err?.message, "String array count exceeds maximum limit");
	});

	await t.step("parseVarint - parses single byte varint", () => {
		const buf = new Uint8Array([42]);
		const [value, n] = parseVarint(buf, 0);
		assertEquals(value, 42);
		assertEquals(n, 1);
	});

	await t.step("parseVarint - parses two byte varint", () => {
		const buf = new Uint8Array([0x43, 0xe8]);
		const [value, n] = parseVarint(buf, 0);
		assertEquals(value, 1000);
		assertEquals(n, 2);
	});

	await t.step("parseVarint - parses four byte varint", () => {
		const buf = new Uint8Array([0x80, 0x01, 0x86, 0xa0]);
		const [value, n] = parseVarint(buf, 0);
		assertEquals(value, 100000);
		assertEquals(n, 4);
	});

	await t.step("parseVarint - parses eight byte varint", async () => {
		const writtenData: Uint8Array[] = [];
		const writerMock = {
			write: spy(async (p: Uint8Array) => {
				writtenData.push(new Uint8Array(p));
				return [p.length, undefined] as [number, Error | undefined];
			}),
		};
		await writeVarint(writerMock, 4294967296);
		const buf = new Uint8Array(writtenData.reduce((a, b) => a + b.length, 0));
		let off = 0;
		for (const d of writtenData) {
			buf.set(d, off);
			off += d.length;
		}
		const [value, n] = parseVarint(buf, 0);
		assertEquals(value, 4294967296);
		assertEquals(n, 8);
	});

	await t.step("parseBytes - parses bytes with length prefix", () => {
		const data = new Uint8Array([3, 1, 2, 3]);
		const [result, n] = parseBytes(data, 0);
		assertEquals(result, new Uint8Array([1, 2, 3]));
		assertEquals(n, 4);
	});

	await t.step("parseString - parses string with length prefix", () => {
		const data = new Uint8Array([5, 104, 101, 108, 108, 111]);
		const [result, n] = parseString(data, 0);
		assertEquals(result, "hello");
		assertEquals(n, 6);
	});

	await t.step("parseStringArray - parses string array", () => {
		const data = new Uint8Array([
			2,
			5,
			104,
			101,
			108,
			108,
			111,
			5,
			119,
			111,
			114,
			108,
			100,
		]);
		const [result, n] = parseStringArray(data, 0);
		assertEquals(result, ["hello", "world"]);
		assertEquals(n, 13);
	});

	// Additional tests for 100% coverage
	await t.step(
		"readVarint - returns error when remaining bytes fail",
		async () => {
			const buffer = Buffer.make(10);
			// Write a varint header indicating 2-byte varint but only 1 byte available
			await buffer.write(new Uint8Array([0x40])); // 0x40 >> 6 = 1, len = 2

			const [value, n, err] = await readVarint(buffer);
			assertEquals(value, 0);
			assertEquals(n, 1);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"writeStringArray - returns error when writeVarint fails",
		async () => {
			const writer = {
				write: spy(async (_p: Uint8Array) => {
					return [0, new Error("Write error")] as [number, Error | undefined];
				}),
			};
			const [n, err] = await writeStringArray(writer, ["test"]);
			assertEquals(n, 0);
			assertEquals(err?.message, "Write error");
		},
	);

	await t.step(
		"writeStringArray - returns error when writeString fails in loop",
		async () => {
			let callCount = 0;
			const writer = {
				write: spy(async (p: Uint8Array) => {
					callCount++;
					// First call succeeds (writeVarint for count), second fails (writeString)
					if (callCount === 1) {
						return [p.length, undefined] as [number, Error | undefined];
					}
					return [0, new Error("Write string error")] as [
						number,
						Error | undefined,
					];
				}),
			};
			const [n, err] = await writeStringArray(writer, ["test"]);
			assertEquals(n > 0, true); // At least the count was written
			assertEquals(err?.message, "Write string error");
		},
	);

	await t.step("readBytes - returns error when readFull fails", async () => {
		const buffer = Buffer.make(10);
		// Write length prefix (3) but no data
		await buffer.write(new Uint8Array([3]));

		const [result, n, err] = await readBytes(buffer);
		assertEquals(result.length, 0);
		assertEquals(n > 0, true);
		assertEquals(err !== undefined, true);
	});

	await t.step(
		"readStringArray - returns error when readString fails in loop",
		async () => {
			const buffer = Buffer.make(10);
			// Write count = 2, then first string length = 3 but no data
			await buffer.write(new Uint8Array([2, 3]));

			const [result, n, err] = await readStringArray(buffer);
			assertEquals(result.length, 0);
			assertEquals(n > 0, true);
			assertEquals(err !== undefined, true);
		},
	);

	await t.step(
		"writeBytes - returns error when writeVarint fails",
		async () => {
			const writer = {
				write: async (_p: Uint8Array) => {
					return [0, new Error("Write failed")] as [number, Error | undefined];
				},
			};
			const [n, err] = await writeBytes(writer, new Uint8Array([1, 2, 3]));
			assertEquals(n, 0);
			assertEquals(err?.message, "Write failed");
		},
	);

	await t.step("writeBytes - returns error when write data fails", async () => {
		let callCount = 0;
		const writer = {
			write: async (p: Uint8Array) => {
				callCount++;
				if (callCount === 1) {
					return [p.length, undefined] as [number, Error | undefined]; // writeVarint succeeds
				}
				return [0, new Error("Write data failed")] as [
					number,
					Error | undefined,
				]; // write data fails
			},
		};
		const [n, err] = await writeBytes(writer, new Uint8Array([1, 2, 3]));
		assertEquals(n > 0, true);
		assertEquals(err?.message, "Write data failed");
	});

	await t.step(
		"writeString - returns error when writeBytes fails",
		async () => {
			const writer = {
				write: async (_p: Uint8Array) => {
					return [0, new Error("Write failed")] as [number, Error | undefined];
				},
			};
			const [n, err] = await writeString(writer, "test");
			assertEquals(n, 0);
			assertEquals(err?.message, "Write failed");
		},
	);

	await t.step("readBytes - returns error when readVarint fails", async () => {
		const buffer = Buffer.make(0); // Empty buffer causes readVarint to fail
		const [result, n, err] = await readBytes(buffer);
		assertEquals(result.length, 0);
		assertEquals(n, 0);
		assertEquals(err !== undefined, true);
	});

	await t.step("readString - returns error when readBytes fails", async () => {
		const buffer = Buffer.make(0); // Empty buffer
		const [result, n, err] = await readString(buffer);
		assertEquals(result, "");
		assertEquals(n, 0);
		assertEquals(err !== undefined, true);
	});

	await t.step(
		"readStringArray - returns error when readVarint fails",
		async () => {
			const buffer = Buffer.make(0); // Empty buffer
			const [result, n, err] = await readStringArray(buffer);
			assertEquals(result.length, 0);
			assertEquals(n, 0);
			assertEquals(err !== undefined, true);
		},
	);
});

Deno.test("putVarint", async (t) => {
	await t.step("encodes each varint width round-trip", () => {
		for (const v of [0, 63, 64, 16383, 16384, 1073741823, 2 ** 40]) {
			const buf = new Uint8Array(8);
			const n = putVarint(buf, 0, v);
			const [got, read] = parseVarint(buf, 0);
			assertEquals(got, v);
			assertEquals(read, n);
		}
	});

	await t.step("throws on a negative value", () => {
		assertThrows(() => putVarint(new Uint8Array(8), 0, -1));
	});

	await t.step("throws on an out-of-range value", () => {
		assertThrows(() => putVarint(new Uint8Array(8), 0, Number.MAX_VALUE));
	});
});

Deno.test("MessageEncoder", async (t) => {
	await t.step("frames body with a varint length prefix", () => {
		const e = new MessageEncoder();
		e.varint(7);
		e.varint(300); // 2-byte varint
		const out = e.frame();

		const [bodyLen, n0] = parseVarint(out, 0);
		assertEquals(bodyLen, out.length - n0);
		const [a, na] = parseVarint(out, n0);
		assertEquals(a, 7);
		const [b] = parseVarint(out, n0 + na);
		assertEquals(b, 300);
	});

	await t.step("round-trips a multi-byte UTF-8 string", () => {
		const e = new MessageEncoder();
		const s = "café—🎬/track";
		e.string(s);
		const out = e.frame();
		const [, n0] = parseVarint(out, 0);
		const [got] = parseString(out, n0);
		assertEquals(got, s);
	});

	await t.step("grows beyond the initial capacity", () => {
		const e = new MessageEncoder(8); // tiny hint forces a grow
		const big = "x".repeat(500);
		e.string(big);
		const out = e.frame();
		const [, n0] = parseVarint(out, 0);
		const [got] = parseString(out, n0);
		assertEquals(got, big);
	});

	await t.step("encodes a value requiring an 8-byte varint", () => {
		const e = new MessageEncoder();
		const v = 2 ** 40;
		e.varint(v);
		const out = e.frame();
		const [, n0] = parseVarint(out, 0);
		const [got] = parseVarint(out, n0);
		assertEquals(got, v);
	});

	await t.step("round-trips uint8 and a string array", () => {
		const e = new MessageEncoder();
		e.uint8(0xff);
		e.stringArray(["/a", "/bb", "/ccc"]);
		const out = e.frame();
		let off = parseVarint(out, 0)[1]; // skip body-length prefix
		const [u, nu] = [out[off]!, 1];
		assertEquals(u, 0xff);
		off += nu;
		const [arr] = parseStringArray(out, off);
		assertEquals(arr, ["/a", "/bb", "/ccc"]);
	});
});

Deno.test("message encode surfaces an invalid-value error", async () => {
	const writer: Writer = {
		write: async (p: Uint8Array): Promise<[number, Error | undefined]> => [p.length, undefined],
	};
	// A negative field makes MessageEncoder.varint throw; encode must catch it
	// and return the error rather than rejecting.
	const err = await new GroupMessage({ sequence: -1 }).encode(writer);
	assertEquals(err instanceof Error, true);
});
