import {
	applyCatalogDelta,
	decodeLocation,
	parseCatalog,
	parseCatalogDelta,
	validateCatalog,
	validateEventTimelineRecord,
	ValidationError,
} from "./mod.ts";
import { assertEquals, assertRejects, assertThrows } from "@std/assert";

Deno.test("parseCatalog rejects delta-only fields", () => {
	assertThrows(
		() => parseCatalog('{"version":1,"deltaUpdate":true}'),
		Error,
		"delta catalog fields are not allowed",
	);
});

Deno.test("parseCatalogDelta rejects independent catalog fields", () => {
	assertThrows(
		() => parseCatalogDelta('{"deltaUpdate":true,"version":1}'),
		Error,
		"independent catalog fields are not allowed",
	);
});

Deno.test("validateCatalog detects duplicate identities via default namespace", () => {
	assertThrows(
		() =>
			validateCatalog({
				defaultNamespace: "demo",
				version: 1,
				tracks: [
					{ name: "v1", packaging: "cmaf" },
					{ namespace: "demo", name: "v1", packaging: "cmaf" },
				],
			}),
		ValidationError,
		"duplicate track identity",
	);
});

Deno.test("applyCatalogDelta respects operation order from source JSON", () => {
	const base = parseCatalog(
		JSON.stringify({
			version: 1,
			tracks: [{ name: "alpha", packaging: "cmaf" }],
		}),
	);
	const delta = parseCatalogDelta(
		'{"deltaUpdate":true,"removeTracks":[{"name":"alpha"}],"addTracks":[{"name":"alpha","packaging":"cmaf"}]}',
	);

	const updated = applyCatalogDelta(base, delta);
	assertEquals(updated.tracks.length, 1);
	assertEquals(updated.tracks[0]?.name, "alpha");
});

Deno.test("applyCatalogDelta blocks default namespace change with inherited tracks", () => {
	const base = parseCatalog(
		JSON.stringify({
			version: 1,
			tracks: [{ name: "alpha", packaging: "cmaf" }],
		}),
	);
	const delta = parseCatalogDelta(
		JSON.stringify({
			deltaUpdate: true,
			generatedAt: 5,
			defaultNamespace: "newns",
			addTracks: [{ name: "beta", packaging: "cmaf" }],
		}),
	);

	assertThrows(
		() => applyCatalogDelta(base, delta),
		Error,
		"cannot change default namespace",
	);
});

Deno.test("decodeLocation requires exactly two numeric fields", () => {
	assertThrows(() => decodeLocation([1]), Error, "exactly 2 items");
	assertThrows(() => decodeLocation([1, "x"]), Error, "must be numbers");
	assertEquals(decodeLocation([5, 9]), { groupId: 5, objectId: 9 });
});

Deno.test("validateEventTimelineRecord enforces selector and payload", async () => {
	await assertRejects(
		async () => {
			validateEventTimelineRecord({});
		},
		Error,
		"exactly one of t, l, or m",
	);

	await assertRejects(
		async () => {
			validateEventTimelineRecord({ t: 1 });
		},
		Error,
		"must contain data",
	);
});
