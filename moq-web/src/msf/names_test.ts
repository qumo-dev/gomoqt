import { assertEquals, assertThrows } from "@std/assert";
import { splitNamespace } from "./mod.ts";

Deno.test("splitNamespace splits at every slash, keeping the fields exactly", () => {
	assertEquals(splitNamespace("conference.example.com/Café/alice"), [
		"conference.example.com",
		"Café",
		"alice",
	]);
});

Deno.test("splitNamespace accepts 32 fields", () => {
	assertEquals(splitNamespace(Array(32).fill("x").join("/")).length, 32);
});

Deno.test("splitNamespace rejects namespaces that break §5.2.2", async (t) => {
	const cases: { name: string; namespace: string; message: string }[] = [
		{ name: "empty", namespace: "", message: "namespace must not be empty" },
		{ name: "leading slash", namespace: "/a", message: "contains an empty field" },
		{ name: "trailing slash", namespace: "a/", message: "contains an empty field" },
		{ name: "doubled slash", namespace: "a//b", message: "contains an empty field" },
		{ name: "33 fields", namespace: Array(33).fill("x").join("/"), message: "has 33 fields" },
		{ name: "NUL", namespace: "a/\u0000", message: "contains a NUL character" },
		{ name: "lone surrogate", namespace: "a/\udc00", message: "is not valid UTF-8" },
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertThrows(() => splitNamespace(c.namespace), Error, c.message);
		});
	}
});
