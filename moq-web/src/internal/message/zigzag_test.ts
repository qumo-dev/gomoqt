import { assertEquals } from "@std/assert";
import { zigzagDecode, zigzagEncode } from "./message.ts";

Deno.test("zigzagEncode maps signed values to unsigned", () => {
	assertEquals(zigzagEncode(0), 0);
	assertEquals(zigzagEncode(-1), 1);
	assertEquals(zigzagEncode(1), 2);
	assertEquals(zigzagEncode(-2), 3);
	assertEquals(zigzagEncode(2), 4);
});

Deno.test("zigzagDecode inverts zigzagEncode", () => {
	const values = [0, 1, -1, 42, -42, 2 ** 50 - 1, -(2 ** 50)];
	for (const v of values) {
		assertEquals(zigzagDecode(zigzagEncode(v)), v);
	}
});
