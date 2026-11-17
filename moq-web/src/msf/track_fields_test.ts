import { assertEquals } from "@std/assert";
import { packagingMetadataValue, streamMapping } from "./mod.ts";

Deno.test("packagingMetadataValue returns the first value of a key", async (t) => {
	const cases: { metadata?: string; key: string; expected?: string }[] = [
		{ metadata: "a=1;b=2;a=3", key: "a", expected: "1" },
		{ metadata: "a=1;b=", key: "b", expected: "" },
		{ metadata: "a=1", key: "A" },
		{ metadata: "", key: "a" },
		{ key: "a" },
	];
	for (const c of cases) {
		await t.step(`${c.key} in ${JSON.stringify(c.metadata)}`, () => {
			assertEquals(
				packagingMetadataValue({ packagingMetadata: c.metadata }, c.key),
				c.expected,
			);
		});
	}
});

Deno.test("streamMapping defaults to group", () => {
	assertEquals(streamMapping({}), "group");
});

Deno.test("streamMapping reads the streamMapping key", () => {
	assertEquals(streamMapping({ packagingMetadata: "x=1;streamMapping=object" }), "object");
});
