// Use real WebTransport-like streams for SendStream/ReceiveStream composition
import { Stream } from "./internal/webtransport/stream.ts";
import { assertEquals } from "@std/assert";
import { BiStreamTypes, UniStreamTypes } from "./stream_type.ts";

Deno.test("Stream", async (t) => {
	await t.step("maps id and substreams correctly", () => {
		const writable = new WritableStream({ write() {} }) as any;
		const readable = new ReadableStream({
			start(c) {
				c.close();
			},
		}) as any;
		const s = new Stream({ streamId: 123n, stream: { writable, readable } });
		assertEquals(s.id, 123n);
		assertEquals(s.readable.id, 123n);
		assertEquals(s.writable.id, 123n);
	});

	// Test BiStreamTypes constant values
	await t.step("BiStreamTypes - Constant Values", async (t) => {
		const cases = {
			"AnnounceStreamType should be 0x01": {
				actual: BiStreamTypes.AnnounceStreamType,
				expected: 0x01,
			},
			"SubscribeStreamType should be 0x02": {
				actual: BiStreamTypes.SubscribeStreamType,
				expected: 0x02,
			},
		};

		for (const [name, c] of Object.entries(cases)) {
			await t.step(name, () => {
				assertEquals(c.actual, c.expected);
			});
		}
	});

	// Test BiStreamTypes properties
	await t.step("BiStreamTypes - Properties", async (t) => {
		await t.step("should be an object", () => {
			assertEquals(typeof BiStreamTypes, "object");
		});

		await t.step("should have all required properties", () => {
			const properties = [
				"AnnounceStreamType",
				"SubscribeStreamType",
			];
			for (const prop of properties) {
				assertEquals(prop in BiStreamTypes, true);
			}
		});

		await t.step("all values should be numbers", () => {
			assertEquals(typeof BiStreamTypes.AnnounceStreamType, "number");
			assertEquals(typeof BiStreamTypes.SubscribeStreamType, "number");
		});

		await t.step("should have unique values", () => {
			const values = Object.values(BiStreamTypes);
			const uniqueValues = new Set(values);
			assertEquals(uniqueValues.size, values.length);
		});
	});

	// Test UniStreamTypes constant values
	await t.step("UniStreamTypes - Constant Values", () => {
		assertEquals(UniStreamTypes.GroupStreamType, 0x00);
	});

	// Test UniStreamTypes properties
	await t.step("UniStreamTypes - Properties", async (t) => {
		await t.step("should be an object", () => {
			assertEquals(typeof UniStreamTypes, "object");
		});

		await t.step("should have GroupStreamType property", () => {
			assertEquals("GroupStreamType" in UniStreamTypes, true);
		});

		await t.step("GroupStreamType should be a number", () => {
			assertEquals(typeof UniStreamTypes.GroupStreamType, "number");
		});
	});

	// // Test Stream Type Integration
	// await t.step("Stream Type Integration - Namespace Overlap", () => {
	// 	// BiStreamTypes and UniStreamTypes can have overlapping values
	// 	// since they represent different categories of streams
	// 	assertEquals(UniStreamTypes.GroupStreamType, 0x00);
	// 	// This is expected and correct - they are in different namespaces
	// });

	// Test switch statement compatibility
	await t.step(
		"Stream Type Integration - Switch Statement Compatibility",
		async (t) => {
			const testBiStreamType = (type: number): string => {
				switch (type) {
					case BiStreamTypes.AnnounceStreamType:
						return "announce";
					case BiStreamTypes.SubscribeStreamType:
						return "subscribe";
					default:
						return "unknown";
				}
			};

			const testUniStreamType = (type: number): string => {
				switch (type) {
					case UniStreamTypes.GroupStreamType:
						return "group";
					default:
						return "unknown";
				}
			};

			await t.step("BiStreamTypes - switch statement cases", async (t) => {
				const biCases = {
					"AnnounceStreamType returns 'announce'": {
						input: BiStreamTypes.AnnounceStreamType,
						expected: "announce",
					},
					"SubscribeStreamType returns 'subscribe'": {
						input: BiStreamTypes.SubscribeStreamType,
						expected: "subscribe",
					},
					"unknown value returns 'unknown'": {
						input: 999,
						expected: "unknown",
					},
				};

				for (const [name, c] of Object.entries(biCases)) {
					await t.step(name, () => {
						assertEquals(testBiStreamType(c.input), c.expected);
					});
				}
			});

			await t.step("UniStreamTypes - switch statement cases", async (t) => {
				const uniCases = {
					"GroupStreamType returns 'group'": {
						input: UniStreamTypes.GroupStreamType,
						expected: "group",
					},
					"unknown value returns 'unknown'": {
						input: 999,
						expected: "unknown",
					},
				};

				for (const [name, c] of Object.entries(uniCases)) {
					await t.step(name, () => {
						assertEquals(testUniStreamType(c.input), c.expected);
					});
				}
			});
		},
	);
});
