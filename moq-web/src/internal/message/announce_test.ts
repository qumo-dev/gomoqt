import { assertEquals, assertInstanceOf } from "@std/assert";
import { Buffer } from "@okdaichi/golikejs/bytes";
import {
	AnnounceEndMessage,
	AnnounceMessageTypes,
	AnnounceStartMessage,
	AnnounceUpdateMessage,
} from "./announce.ts";
import type { AnnounceMessage } from "./announce.ts";
import { MessageDecodeError } from "./message.ts";

Deno.test("announcements round-trip their body", async (t) => {
	const cases: { name: string; msg: AnnounceMessage; fresh: () => AnnounceMessage }[] = [
		{
			name: "ANNOUNCE_START with hops and costs",
			msg: new AnnounceStartMessage({
				suffix: "a",
				hopIDs: [7, 0],
				warmCost: 3,
				coldCost: 4,
			}),
			fresh: () => new AnnounceStartMessage(),
		},
		{
			name: "ANNOUNCE_START with zero values",
			msg: new AnnounceStartMessage(),
			fresh: () => new AnnounceStartMessage(),
		},
		{
			name: "ANNOUNCE_END with a two-byte Announce ID",
			msg: new AnnounceEndMessage({ announceId: 300 }),
			fresh: () => new AnnounceEndMessage(),
		},
		{
			name: "ANNOUNCE_UPDATE with hops and costs",
			msg: new AnnounceUpdateMessage({
				announceId: 2,
				hopIDs: [9],
				warmCost: 1,
				coldCost: 2,
			}),
			fresh: () => new AnnounceUpdateMessage(),
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const buf = Buffer.make(64);
			await c.msg.encode(buf);
			const decoded = c.fresh();

			const err = await decoded.decode(buf);

			assertEquals(err, undefined);
			assertEquals(decoded, c.msg);
		});
	}
});

Deno.test("each announcement carries its moq-lite-06 Type tag", () => {
	const cases = [
		{ msg: new AnnounceStartMessage(), type: 0x0 },
		{ msg: new AnnounceEndMessage(), type: 0x1 },
		{ msg: new AnnounceUpdateMessage(), type: 0x2 },
	];
	for (const c of cases) {
		assertEquals(c.msg.type, c.type);
	}
	assertEquals(AnnounceMessageTypes, { Start: 0x0, End: 0x1, Update: 0x2 });
});

Deno.test("a malformed announcement body is a MessageDecodeError", async (t) => {
	const cases: { name: string; bytes: number[]; fresh: () => AnnounceMessage }[] = [
		{
			// length 4: suffix "", Hop Count 3, one Hop ID, warm cost; no cold cost
			name: "Hop Count larger than its entries",
			bytes: [0x04, 0x00, 0x03, 0x05, 0x00],
			fresh: () => new AnnounceStartMessage(),
		},
		{
			// length 2: suffix length 5 with a single byte of suffix
			name: "suffix longer than the body",
			bytes: [0x02, 0x05, 0x61],
			fresh: () => new AnnounceStartMessage(),
		},
		{
			// length 1: the first byte of a two-byte varint
			name: "truncated Announce ID",
			bytes: [0x01, 0x41],
			fresh: () => new AnnounceEndMessage(),
		},
		{
			// length 2: Announce ID 1, then a stray byte
			name: "trailing bytes after the last field",
			bytes: [0x02, 0x01, 0x00],
			fresh: () => new AnnounceEndMessage(),
		},
		{
			// length 3: Announce ID, Hop Count 0, warm cost; no cold cost
			name: "missing Cold Route Cost",
			bytes: [0x03, 0x01, 0x00, 0x00],
			fresh: () => new AnnounceUpdateMessage(),
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const buf = Buffer.make(16);
			await buf.write(new Uint8Array(c.bytes));

			const err = await c.fresh().decode(buf);

			assertInstanceOf(err, MessageDecodeError);
		});
	}
});
