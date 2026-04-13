import {
	applyCatalogDelta,
	decodeLocation,
	effectiveNamespace,
	parseCatalog,
	parseCatalogDelta,
	parseTrack,
	stringifyCatalog,
	trackId,
	validateCatalog,
	validateEventTimelineRecord,
	validateTrack,
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

Deno.test("parseCatalog rejects invalid track field types", () => {
	assertThrows(
		() =>
			parseCatalog(
				'{"version":1,"tracks":[{"name":"alpha","packaging":"cmaf","bitrate":"fast"}]}',
			),
		Error,
		"tracks.0.bitrate",
	);
});

Deno.test("parseCatalogDelta rejects independent catalog fields", () => {
	assertThrows(
		() => parseCatalogDelta('{"deltaUpdate":true,"version":1}'),
		Error,
		"independent catalog fields are not allowed",
	);
});

Deno.test("parseCatalogDelta rejects invalid removeTrack field types", () => {
	assertThrows(
		() => parseCatalogDelta('{"deltaUpdate":true,"removeTracks":[{"name":1}]}'),
		Error,
		"name",
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

Deno.test("catalog helpers cover namespace and serialization branches", () => {
	assertEquals(effectiveNamespace({ namespace: "ns" }, "fallback"), "ns");
	assertEquals(effectiveNamespace({}, "fallback"), "fallback");
	assertEquals(effectiveNamespace({}, undefined), "\u0000catalog");
	assertEquals(trackId({ name: "v" }, "fallback"), "fallback|v");

	const parsedTrack = parseTrack(
		{
			name: "clip",
			packaging: "cmaf",
			extra: 1,
		},
	);
	assertEquals(parsedTrack.extraFields, { extra: 1 });

	assertEquals(
		stringifyCatalog({
			version: 2,
			generatedAt: 10,
			isComplete: true,
			tracks: [parsedTrack],
			extraFields: { hello: "world" },
		}),
		JSON.stringify({
			hello: "world",
			version: 2,
			generatedAt: 10,
			isComplete: true,
			tracks: [{ extra: 1, name: "clip", packaging: "cmaf", extraFields: { extra: 1 } }],
		}),
	);
});

Deno.test("validateTrack covers packaging-specific validation branches", () => {
	assertEquals(
		validateTrack({ name: "t", packaging: "loc", isLive: true, trackDuration: 1 }, "t"),
		["t: trackDuration must not be present when isLive is true"],
	);
	assertEquals(
		validateTrack(
			{
				name: "e",
				packaging: "eventtimeline",
				mimeType: "application/json",
				depends: ["base"],
			},
			"tracks[0]",
		),
		[
			"tracks[0]: eventType is required for eventtimeline tracks",
		],
	);
	assertEquals(
		validateTrack(
			{ name: "m", packaging: "mediatimeline", eventType: "x", mimeType: "text/plain" },
			"tracks[0]",
		),
		[
			"tracks[0]: eventType must not be set for mediatimeline tracks",
			"tracks[0]: mediatimeline tracks must use mimeType application/json",
			"tracks[0]: mediatimeline tracks must declare depends",
		],
	);
	assertEquals(
		validateTrack({ name: "l", packaging: "loc", isLive: false }, "tracks[0]"),
		[],
	);
	assertEquals(
		validateTrack({ name: "d", packaging: "cmaf", eventType: "x" }, "tracks[0]"),
		["tracks[0]: eventType must only be set for eventtimeline tracks"],
	);
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
