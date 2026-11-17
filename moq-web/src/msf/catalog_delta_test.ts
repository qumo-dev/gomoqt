import { assertEquals, assertThrows } from "@std/assert";
import {
	applyCatalogDelta,
	type Catalog,
	CatalogCompleteError,
	type CatalogDelta,
	parseCatalogDelta,
	stringifyCatalogDelta,
	type Track,
	validateCatalogDelta,
	ValidationError,
} from "./mod.ts";

function delta(fields: Partial<CatalogDelta>): CatalogDelta {
	return {
		generatedAt: 1,
		addTracks: [],
		removeTracks: [],
		cloneTracks: [],
		updateTracks: [],
		...fields,
	};
}

function track(name: string, fields: Partial<Track> = {}): Track {
	return { name, packaging: "cmaf", isLive: true, ...fields };
}

function base(...tracks: Track[]): Catalog {
	return { version: "draft-02", tracks };
}

function problemsOf(value: CatalogDelta): string[] {
	try {
		validateCatalogDelta(value);
	} catch (err) {
		if (err instanceof ValidationError) {
			return err.problems;
		}
		throw err;
	}
	return [];
}

// ─── parseCatalogDelta ───────────────────────────────────────────────────────

Deno.test("parseCatalogDelta rejects malformed deltas", async (t) => {
	const cases: { name: string; input: string; message: string }[] = [
		...["version", "isComplete", "tracks", "publishTracks", "initDataList"].map((key) => ({
			name: `root field ${key}`,
			input: `{"deltaUpdate":[],"${key}":null}`,
			message: `root catalog field "${key}" is not allowed in a delta catalog`,
		})),
		{
			name: "no deltaUpdate",
			input: '{"generatedAt":1}',
			message: "must include a deltaUpdate",
		},
		{
			name: "deltaUpdate not an array",
			input: '{"deltaUpdate":false}',
			message: "deltaUpdate",
		},
		{
			name: "unknown op",
			input: '{"deltaUpdate":[{"op":"rename","tracks":[]}]}',
			message: 'unknown delta update op "rename"',
		},
		{
			name: "op missing",
			input: '{"deltaUpdate":[{"tracks":[]}]}',
			message: "must contain an op field",
		},
		{
			name: "tracks missing",
			input: '{"deltaUpdate":[{"op":"add"}]}',
			message: "must contain a tracks field",
		},
		{
			name: "tracks not an array",
			input: '{"deltaUpdate":[{"op":"add","tracks":123}]}',
			message: "tracks",
		},
		{
			name: "empty remove namespace",
			input: '{"deltaUpdate":[{"op":"remove","tracks":[{"name":"a","namespace":""}]}]}',
			message: "namespace must not be empty",
		},
		{
			name: "empty parentNamespace",
			input:
				'{"deltaUpdate":[{"op":"update","tracks":[{"parentName":"a","parentNamespace":""}]}]}',
			message: "parentNamespace must not be empty",
		},
		{
			name: "BOM",
			input: '﻿{"deltaUpdate":[]}',
			message: "byte order mark",
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertThrows(() => parseCatalogDelta(c.input), Error, c.message);
		});
	}
});

Deno.test("parseCatalogDelta parses every operation kind", () => {
	const parsed = parseCatalogDelta(
		JSON.stringify({
			generatedAt: 5,
			deltaUpdate: [
				{ op: "add", tracks: [{ name: "a", packaging: "cmaf", isLive: true }] },
				{ op: "remove", tracks: [{ name: "b", namespace: "ns", extra: 1 }] },
				{ op: "clone", tracks: [{ name: "c", parentName: "a", parentNamespace: "p" }] },
				{ op: "update", tracks: [{ parentName: "a", bitrate: 1 }] },
			],
		}),
	);

	assertEquals(parsed, {
		generatedAt: 5,
		addTracks: [{ name: "a", packaging: "cmaf", isLive: true }],
		removeTracks: [{ name: "b", namespace: "ns", extraFields: { extra: 1 } }],
		cloneTracks: [{ track: { name: "c" }, parentName: "a", parentNamespace: "p" }],
		updateTracks: [{ track: { bitrate: 1 }, parentName: "a" }],
		deltaOps: [
			{ kind: "addTracks", count: 1 },
			{ kind: "removeTracks", count: 1 },
			{ kind: "cloneTracks", count: 1 },
			{ kind: "updateTracks", count: 1 },
		],
	});
});

Deno.test("parseCatalogDelta uses the last of repeated deltaUpdate keys", () => {
	const parsed = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"remove","tracks":[{"name":"a"}]}],"deltaUpdate":[{"op":"remove","tracks":[{"name":"b"}]}]}',
	);

	assertEquals(parsed.removeTracks, [{ name: "b" }]);
});

Deno.test("parseCatalogDelta keeps custom root fields, defaultNamespace among them", () => {
	const parsed = parseCatalogDelta('{"deltaUpdate":[],"defaultNamespace":"ns","custom":1}');

	assertEquals([parsed.defaultNamespace, parsed.extraFields], [undefined, {
		defaultNamespace: "ns",
		custom: 1,
	}]);
});

Deno.test("parseCatalogDelta reads a null generatedAt as absent", () => {
	assertEquals(parseCatalogDelta('{"deltaUpdate":[],"generatedAt":null}').generatedAt, undefined);
});

// ─── validateCatalogDelta ────────────────────────────────────────────────────

Deno.test("validateCatalogDelta reports the delta rules of draft-02", async (t) => {
	const cases: { name: string; delta: CatalogDelta; problems: string[] }[] = [
		{
			name: "valid",
			delta: delta({ removeTracks: [{ name: "a" }] }),
			problems: [],
		},
		{
			name: "no generatedAt and no tracks",
			delta: delta({ generatedAt: undefined }),
			problems: [
				"delta catalog must include generatedAt",
				"delta catalog must contain at least one add, remove, clone, or update track",
			],
		},
		{
			name: "invalid add",
			delta: delta({ addTracks: [{ name: "a", packaging: "cmaf" }] }),
			problems: ["add[0]: isLive is required"],
		},
		{
			name: "remove entry with other fields",
			delta: delta({
				removeTracks: [{ name: "", namespace: "a//b", extraFields: { x: 1 } }],
			}),
			problems: [
				"remove[0]: name is required",
				'remove[0]: namespace "a//b" contains an empty field',
				"remove[0]: remove track entries may contain only name and optional namespace",
			],
		},
		{
			name: "clone without name or parentName",
			delta: delta({ cloneTracks: [{ track: {} }] }),
			problems: [
				"clone[0]: name is required",
				"clone[0]: parentName is required for clone tracks",
			],
		},
		{
			name: "update with a bad parent",
			delta: delta({
				updateTracks: [{ track: {}, parentName: "a\u0000", parentNamespace: "/x" }],
			}),
			problems: [
				'update[0]: parentName: "a\\u0000" contains a NUL character',
				'update[0]: parentNamespace: namespace "/x" contains an empty field',
			],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(c.delta), c.problems);
		});
	}
});

// ─── applyCatalogDelta ───────────────────────────────────────────────────────

Deno.test("applyCatalogDelta applies interleaved operations in wire order", () => {
	const parsed = parseCatalogDelta(
		JSON.stringify({
			generatedAt: 2,
			deltaUpdate: [
				{ op: "remove", tracks: [{ name: "a" }] },
				{
					op: "add",
					tracks: [{ name: "a", packaging: "cmaf", isLive: true, label: "new" }],
				},
				{ op: "remove", tracks: [{ name: "b" }] },
			],
		}),
	);

	const result = applyCatalogDelta(base(track("a"), track("b")), parsed);

	assertEquals(result.tracks, [track("a", { label: "new" })]);
});

Deno.test("applyCatalogDelta applies code-built operations as add, remove, clone, update", () => {
	const value = delta({
		updateTracks: [{ track: { label: "updated" }, parentName: "c" }],
		cloneTracks: [{ track: { name: "c" }, parentName: "a" }],
		addTracks: [track("a")],
	});

	const result = applyCatalogDelta(base(), value);

	assertEquals(result.tracks, [track("a"), track("c", { label: "updated" })]);
});

Deno.test("applyCatalogDelta clones a parent with overrides", () => {
	const parent = track("hd", { codec: "c1", bitrate: 5, depends: ["x"], extraFields: { v: 1 } });
	const value = delta({
		cloneTracks: [{
			track: { name: "sd", bitrate: 1, label: "", extraFields: { w: 2 } },
			parentName: "hd",
		}],
	});

	const result = applyCatalogDelta(base(track("x"), parent), value);

	assertEquals(
		result.tracks[2],
		track("sd", { codec: "c1", bitrate: 1, depends: ["x"], extraFields: { v: 1, w: 2 } }),
	);
});

Deno.test("applyCatalogDelta looks for a parent in parentNamespace, not the clone's own", () => {
	const catalog: Catalog = {
		...base(track("src", { namespace: "other", codec: "c1" })),
		defaultNamespace: "home",
	};
	const value = delta({
		cloneTracks: [{
			track: { namespace: "home", name: "copy" },
			parentName: "src",
			parentNamespace: "other",
		}],
	});

	const result = applyCatalogDelta(catalog, value);

	assertEquals(result.tracks[1]?.codec, "c1");
});

Deno.test("applyCatalogDelta renames a track and ends it with an update", () => {
	const value = delta({
		updateTracks: [{ track: { name: "b", isLive: false, trackDuration: 10 }, parentName: "a" }],
	});

	const result = applyCatalogDelta(base(track("a")), value);

	assertEquals(result.tracks, [track("b", { isLive: false, trackDuration: 10 })]);
});

Deno.test("applyCatalogDelta rejects operations that do not fit the catalog", async (t) => {
	const cases: { name: string; catalog: Catalog; delta: CatalogDelta; message: string }[] = [
		{
			name: "add a duplicate",
			catalog: base(track("a")),
			delta: delta({ addTracks: [track("a")] }),
			message: 'cannot add duplicate track "a" at index 0',
		},
		{
			name: "remove an unknown track",
			catalog: base(),
			delta: delta({ removeTracks: [{ name: "a" }] }),
			message: 'cannot remove unknown track "a"',
		},
		{
			name: "clone an unknown parent",
			catalog: base(),
			delta: delta({ cloneTracks: [{ track: { name: "b" }, parentName: "a" }] }),
			message: 'cannot clone unknown parent track "a"',
		},
		{
			name: "clone into a duplicate",
			catalog: base(track("a"), track("b")),
			delta: delta({ cloneTracks: [{ track: { name: "b" }, parentName: "a" }] }),
			message: 'cannot clone into duplicate track "b" at index 1',
		},
		{
			name: "update an unknown track",
			catalog: base(),
			delta: delta({ updateTracks: [{ track: {}, parentName: "a" }] }),
			message: 'cannot update unknown track "a"',
		},
		{
			name: "update isLive from false to true",
			catalog: base(track("a", { isLive: false })),
			delta: delta({ updateTracks: [{ track: { isLive: true }, parentName: "a" }] }),
			message: 'cannot update track "a": isLive must not change from false to true',
		},
		{
			name: "update a rename onto another track",
			catalog: base(track("a"), track("b")),
			delta: delta({ updateTracks: [{ track: { name: "b" }, parentName: "a" }] }),
			message: 'cannot update track "a" into duplicate track "b" at index 1',
		},
		{
			name: "change the default namespace a publish track inherits",
			catalog: {
				...base(track("a", { namespace: "x" })),
				publishTracks: [track("p")],
			},
			delta: delta({ defaultNamespace: "y", removeTracks: [{ name: "a", namespace: "x" }] }),
			message: "cannot change default namespace",
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertThrows(() => applyCatalogDelta(c.catalog, c.delta), Error, c.message);
		});
	}
});

Deno.test("applyCatalogDelta rejects any delta on a complete catalog", () => {
	const catalog: Catalog = { ...base(track("a")), isComplete: true };

	assertThrows(
		() => applyCatalogDelta(catalog, delta({ removeTracks: [{ name: "a" }] })),
		CatalogCompleteError,
	);
});

Deno.test("applyCatalogDelta rejects removing a track another depends on", () => {
	const catalog = base(track("video"), track("timeline", { depends: ["video"] }));

	assertThrows(
		() => applyCatalogDelta(catalog, delta({ removeTracks: [{ name: "video" }] })),
		ValidationError,
		'track name "video" does not match any other track',
	);
});

Deno.test("applyCatalogDelta merges generatedAt and extra fields", () => {
	const catalog: Catalog = { ...base(track("a")), generatedAt: 1, extraFields: { keep: 1 } };
	const value = delta({ generatedAt: 9, extraFields: { add: 2 }, addTracks: [track("b")] });

	const result = applyCatalogDelta(catalog, value);

	assertEquals([result.generatedAt, result.extraFields], [9, { keep: 1, add: 2 }]);
});

Deno.test("applyCatalogDelta does not change its arguments", () => {
	const catalog = base(track("a"));
	const value = delta({ updateTracks: [{ track: { label: "x" }, parentName: "a" }] });
	const before = structuredClone([catalog, value]);

	applyCatalogDelta(catalog, value);

	assertEquals([catalog, value], before);
});

// ─── stringifyCatalogDelta ───────────────────────────────────────────────────

Deno.test("stringifyCatalogDelta writes code-built operations in add, remove, clone, update order", () => {
	const value = delta({
		defaultNamespace: "not/written",
		updateTracks: [{ track: { label: "u" }, parentName: "c", parentNamespace: "ns" }],
		cloneTracks: [{ track: { name: "c" }, parentName: "a" }],
		removeTracks: [{ name: "r", namespace: "ns" }],
		addTracks: [track("a")],
		extraFields: { custom: true },
	});

	assertEquals(JSON.parse(stringifyCatalogDelta(value)), {
		custom: true,
		generatedAt: 1,
		deltaUpdate: [
			{ op: "add", tracks: [{ name: "a", packaging: "cmaf", isLive: true }] },
			{ op: "remove", tracks: [{ name: "r", namespace: "ns" }] },
			{ op: "clone", tracks: [{ name: "c", parentName: "a" }] },
			{ op: "update", tracks: [{ label: "u", parentName: "c", parentNamespace: "ns" }] },
		],
	});
});

Deno.test("stringifyCatalogDelta follows recorded operations with tracks added after parsing", () => {
	const parsed = parseCatalogDelta('{"deltaUpdate":[{"op":"remove","tracks":[{"name":"a"}]}]}');
	parsed.removeTracks.push({ name: "b" });
	parsed.addTracks.push(track("c"));

	assertEquals(JSON.parse(stringifyCatalogDelta(parsed)).deltaUpdate, [
		{ op: "remove", tracks: [{ name: "a" }] },
		{ op: "add", tracks: [{ name: "c", packaging: "cmaf", isLive: true }] },
		{ op: "remove", tracks: [{ name: "b" }] },
	]);
});
