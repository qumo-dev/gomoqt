import { assertEquals, assertThrows } from "@std/assert";
import {
	applyCatalogDelta,
	type CatalogDelta,
	parseCatalog,
	parseCatalogDelta,
	stringifyCatalogDelta,
	type TrackClone,
	type TrackRef,
	validateCatalogDelta,
} from "./mod.ts";
import { ValidationError } from "./mod.ts";

// ─── parseCatalogDelta ───────────────────────────────────────────────────────

Deno.test("parseCatalogDelta rejects malformed JSON schema", () => {
	assertThrows(
		() => parseCatalogDelta('{"deltaUpdate":[{"op":"add","tracks":123}]}'),
		Error,
		"deltaUpdate.0.tracks",
	);
});

Deno.test("parseCatalogDelta parses addTracks", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"add","tracks":[{"name":"video","packaging":"cmaf"}]}]}',
	);
	assertEquals(delta.addTracks.length, 1);
	assertEquals(delta.addTracks[0]?.name, "video");
	assertEquals(delta.removeTracks.length, 0);
	assertEquals(delta.cloneTracks.length, 0);
});

Deno.test("parseCatalogDelta parses removeTracks with extraFields", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"remove","tracks":[{"name":"video","extra":1}]}]}',
	);
	assertEquals(delta.removeTracks.length, 1);
	assertEquals(delta.removeTracks[0]?.name, "video");
	assertEquals(delta.removeTracks[0]?.extraFields, { extra: 1 });
});

Deno.test("parseCatalogDelta parses cloneTracks with parentName", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"clone","tracks":[{"parentName":"video","name":"audio","packaging":"cmaf"}]}]}',
	);
	assertEquals(delta.cloneTracks.length, 1);
	assertEquals(delta.cloneTracks[0]?.parentName, "video");
	assertEquals(delta.cloneTracks[0]?.track.name, "audio");
});

Deno.test("parseCatalogDelta parses optional fields and extraFields", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"add","tracks":[{"name":"v","packaging":"cmaf"}]}],"defaultNamespace":"ns","generatedAt":42,"isComplete":true,"custom":"value"}',
	);
	assertEquals(delta.defaultNamespace, "ns");
	assertEquals(delta.generatedAt, 42);
	assertEquals(delta.isComplete, true);
	assertEquals(delta.extraFields, { custom: "value" });
});

Deno.test("parseCatalogDelta accepts Uint8Array input", () => {
	const json = '{"deltaUpdate":[{"op":"remove","tracks":[{"name":"v"}]}]}';
	const bytes = new TextEncoder().encode(json);
	const delta = parseCatalogDelta(bytes);
	assertEquals(delta.removeTracks[0]?.name, "v");
});

Deno.test("parseCatalogDelta records deltaOpOrder from JSON key order", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"remove","tracks":[{"name":"a"}]},{"op":"add","tracks":[{"name":"b","packaging":"cmaf"}]}]}',
	);
	assertEquals(delta.deltaOpOrder, ["removeTracks", "addTracks"]);
});

// ─── validateCatalogDelta ────────────────────────────────────────────────────

Deno.test("validateCatalogDelta rejects empty operations", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [],
				removeTracks: [],
				cloneTracks: [],
			}),
		ValidationError,
		"must contain addTracks",
	);
});

Deno.test("validateCatalogDelta rejects invalid addTrack", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [{ name: "", packaging: "cmaf" }],
				removeTracks: [],
				cloneTracks: [],
			}),
		ValidationError,
		"addTracks[0]",
	);
});

Deno.test("validateCatalogDelta rejects removeTrack missing name", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [],
				removeTracks: [{ name: "" }],
				cloneTracks: [],
			}),
		ValidationError,
		"name is required",
	);
});

Deno.test("validateCatalogDelta rejects removeTrack with extraFields", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [],
				removeTracks: [{ name: "v", extraFields: { bad: 1 } }],
				cloneTracks: [],
			}),
		ValidationError,
		"may contain only name",
	);
});

Deno.test("validateCatalogDelta rejects cloneTrack missing name", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [],
				removeTracks: [],
				cloneTracks: [{ track: { name: "", packaging: "cmaf" }, parentName: "parent" }],
			}),
		ValidationError,
		"name is required",
	);
});

Deno.test("validateCatalogDelta rejects cloneTrack missing parentName", () => {
	assertThrows(
		() =>
			validateCatalogDelta({
				addTracks: [],
				removeTracks: [],
				cloneTracks: [{ track: { name: "derived", packaging: "cmaf" } }],
			}),
		ValidationError,
		"parentName is required",
	);
});

Deno.test("validateCatalogDelta accepts valid delta", () => {
	validateCatalogDelta({
		addTracks: [{ name: "video", packaging: "cmaf" }],
		removeTracks: [],
		cloneTracks: [],
	});
});

// ─── applyCatalogDelta ───────────────────────────────────────────────────────

Deno.test("applyCatalogDelta adds tracks", () => {
	const base = parseCatalog('{"version":1,"tracks":[{"name":"audio","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [{ name: "video", packaging: "cmaf" }],
		removeTracks: [],
		cloneTracks: [],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.tracks.length, 2);
	assertEquals(result.tracks[1]?.name, "video");
});

Deno.test("applyCatalogDelta removes tracks", () => {
	const base = parseCatalog('{"version":1,"tracks":[{"name":"video","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [{ name: "video" }],
		cloneTracks: [],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.tracks.length, 0);
});

Deno.test("applyCatalogDelta clones tracks with overrides", () => {
	const base = parseCatalog(
		'{"version":1,"tracks":[{"name":"video","packaging":"cmaf","bitrate":1000}]}',
	);
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [
			{
				track: { name: "video-low", packaging: "cmaf", bitrate: 500 },
				parentName: "video",
			},
		],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.tracks.length, 2);
	assertEquals(result.tracks[1]?.name, "video-low");
	assertEquals(result.tracks[1]?.bitrate, 500);
});

Deno.test("applyCatalogDelta clones tracks with depends array", () => {
	const base = parseCatalog(
		'{"version":1,"tracks":[{"name":"mediatimeline","packaging":"mediatimeline","mimeType":"application/json","depends":["video"]}]}',
	);
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [
			{
				track: {
					name: "mediatimeline-2",
					packaging: "mediatimeline",
					mimeType: "application/json",
					depends: ["video", "audio"],
				},
				parentName: "mediatimeline",
			},
		],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.tracks[1]?.depends, ["video", "audio"]);
});

Deno.test("applyCatalogDelta clones tracks with extraFields overrides", () => {
	const base = parseCatalog(
		'{"version":1,"tracks":[{"orig":1,"name":"video","packaging":"cmaf"}]}',
	);
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [
			{
				track: { name: "video-hd", packaging: "cmaf", extraFields: { hd: true } },
				parentName: "video",
			},
		],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.tracks[1]?.extraFields?.hd, true);
});

Deno.test("applyCatalogDelta updates defaultNamespace, generatedAt, isComplete", () => {
	const base = parseCatalog(
		'{"version":1,"tracks":[{"namespace":"ns","name":"v","packaging":"cmaf"}]}',
	);
	const delta: CatalogDelta = {
		defaultNamespace: "new-ns",
		generatedAt: 99,
		isComplete: true,
		addTracks: [{ name: "a", packaging: "cmaf" }],
		removeTracks: [],
		cloneTracks: [],
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.defaultNamespace, "new-ns");
	assertEquals(result.generatedAt, 99);
	assertEquals(result.isComplete, true);
});

Deno.test("applyCatalogDelta merges extraFields from delta", () => {
	const base = parseCatalog('{"a":1,"version":1,"tracks":[{"name":"v","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [{ name: "v" }],
		cloneTracks: [],
		extraFields: { b: 2 },
	};
	const result = applyCatalogDelta(base, delta);
	assertEquals(result.extraFields?.a, 1);
	assertEquals(result.extraFields?.b, 2);
});

Deno.test("applyCatalogDelta rejects duplicate addTrack", () => {
	const base = parseCatalog('{"version":1,"tracks":[{"name":"v","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [{ name: "v", packaging: "cmaf" }],
		removeTracks: [],
		cloneTracks: [],
	};
	assertThrows(() => applyCatalogDelta(base, delta), Error, "cannot add duplicate");
});

Deno.test("applyCatalogDelta rejects remove of unknown track", () => {
	const base = parseCatalog('{"version":1,"tracks":[{"name":"v","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [{ name: "unknown" }],
		cloneTracks: [],
	};
	assertThrows(() => applyCatalogDelta(base, delta), Error, "cannot remove unknown");
});

Deno.test("applyCatalogDelta rejects clone of unknown parent", () => {
	const base = parseCatalog('{"version":1,"tracks":[{"name":"v","packaging":"cmaf"}]}');
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [{ track: { name: "derived", packaging: "cmaf" }, parentName: "missing" }],
	};
	assertThrows(() => applyCatalogDelta(base, delta), Error, "cannot clone unknown parent");
});

Deno.test("applyCatalogDelta rejects clone into duplicate track", () => {
	const base = parseCatalog(
		'{"version":1,"tracks":[{"name":"video","packaging":"cmaf"},{"name":"audio","packaging":"cmaf"}]}',
	);
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [{ track: { name: "audio", packaging: "cmaf" }, parentName: "video" }],
	};
	assertThrows(() => applyCatalogDelta(base, delta), Error, "cannot clone into duplicate");
});

// ─── stringifyCatalogDelta ───────────────────────────────────────────────────

Deno.test("stringifyCatalogDelta roundtrip basic delta", () => {
	const delta: CatalogDelta = {
		addTracks: [{ name: "video", packaging: "cmaf" }],
		removeTracks: [],
		cloneTracks: [],
	};
	const json = stringifyCatalogDelta(delta);
	const parsed = JSON.parse(json);
	assertEquals(parsed.deltaUpdate, [{
		op: "add",
		tracks: [{ name: "video", packaging: "cmaf" }],
	}]);
});

Deno.test("stringifyCatalogDelta includes optional fields", () => {
	const delta: CatalogDelta = {
		defaultNamespace: "ns",
		generatedAt: 5,
		isComplete: true,
		addTracks: [],
		removeTracks: [{ name: "v", namespace: "ns" }],
		cloneTracks: [{ track: { name: "clone", packaging: "cmaf" }, parentName: "orig" }],
		extraFields: { custom: "x" },
	};
	const json = stringifyCatalogDelta(delta);
	const parsed = JSON.parse(json);
	assertEquals(parsed.defaultNamespace, "ns");
	assertEquals(parsed.generatedAt, 5);
	assertEquals(parsed.isComplete, true);
	assertEquals(parsed.deltaUpdate, [
		{ op: "remove", tracks: [{ name: "v", namespace: "ns" }] },
		{ op: "clone", tracks: [{ name: "clone", packaging: "cmaf", parentName: "orig" }] },
	]);
	assertEquals(parsed.custom, "x");
});

Deno.test("stringifyCatalogDelta omits empty arrays", () => {
	const delta: CatalogDelta = {
		removeTracks: [{ name: "v" }],
		addTracks: [],
		cloneTracks: [],
	};
	const json = stringifyCatalogDelta(delta);
	const parsed = JSON.parse(json);
	// Only non-empty operations are written, one {op, tracks} entry each.
	assertEquals(parsed.deltaUpdate, [{ op: "remove", tracks: [{ name: "v" }] }]);
});

// ─── TrackRef / TrackClone type checking ────────────────────────────────────

Deno.test("TrackRef and TrackClone are typed correctly", () => {
	const ref: TrackRef = { name: "v", namespace: "ns" };
	const clone: TrackClone = { track: { name: "c", packaging: "cmaf" }, parentName: "v" };
	assertEquals(ref.name, "v");
	assertEquals(clone.parentName, "v");
});
