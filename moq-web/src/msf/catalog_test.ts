import { assertEquals, assertThrows } from "@std/assert";
import {
	type Catalog,
	effectiveNamespace,
	initDataPropertyType,
	parseCatalog,
	parseTrack,
	stringifyCatalog,
	type Track,
	trackId,
	validateCatalog,
	ValidationError,
} from "./mod.ts";

function problemsOf(catalog: Catalog): string[] {
	try {
		validateCatalog(catalog);
	} catch (err) {
		if (err instanceof ValidationError) {
			return err.problems;
		}
		throw err;
	}
	return [];
}

function catalogOf(tracks: Track[], extra: Partial<Catalog> = {}): Catalog {
	return { version: "draft-02", tracks, ...extra };
}

const video: Track = {
	name: "video",
	packaging: "loc",
	isLive: true,
	renderGroup: 1,
};

// ─── parseCatalog ────────────────────────────────────────────────────────────

Deno.test("parseCatalog rejects malformed documents", async (t) => {
	const bom = new Uint8Array([0xef, 0xbb, 0xbf, ...new TextEncoder().encode('{"tracks":[]}')]);
	const cases: { name: string; input: string | Uint8Array; message: string }[] = [
		{ name: "delta-only field", input: '{"deltaUpdate":[]}', message: "delta catalog fields" },
		{ name: "BOM in bytes", input: bom, message: "byte order mark" },
		{ name: "BOM in text", input: '﻿{"tracks":[]}', message: "byte order mark" },
		{
			name: "invalid UTF-8",
			input: new Uint8Array([0x7b, 0x22, 0xff, 0x22, 0x3a, 0x31, 0x7d]),
			message: "not valid UTF-8",
		},
		{
			name: "unpaired surrogate escape in a value",
			input: '{"tracks":[{"name":"a\\ud800"}]}',
			message: "unpaired surrogate",
		},
		{
			name: "unpaired surrogate escape in a key",
			input: '{"\\udc00":1,"tracks":[]}',
			message: "unpaired surrogate",
		},
		{ name: "not an object", input: "[]", message: "expected JSON object" },
		{
			name: "wrong field type",
			input: '{"tracks":[{"name":"a","bitrate":"fast"}]}',
			message: "bitrate",
		},
		{
			name: "non-integer number",
			input: '{"tracks":[{"name":"a","framerate":29.97}]}',
			message: "framerate",
		},
		{
			name: "explicitly empty namespace",
			input: '{"tracks":[{"namespace":"","name":"a"}]}',
			message: "namespace must not be empty",
		},
		{
			name: "depends entry that is neither string nor number",
			input: '{"tracks":[{"name":"a","depends":[null]}]}',
			message: "depends",
		},
		{
			name: "template of the wrong length",
			input: '{"tracks":[{"name":"a","template":[0,1]}]}',
			message: "exactly 6 items",
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertThrows(() => parseCatalog(c.input), Error, c.message);
		});
	}
});

Deno.test("parseCatalog keeps a paired surrogate escape", () => {
	const catalog = parseCatalog('{"tracks":[{"name":"\\ud83c\\udfa5"}]}');

	assertEquals(catalog.tracks[0]?.name, "\u{1f3a5}");
});

Deno.test("parseCatalog reads a null known field as absent", () => {
	const catalog = parseCatalog(
		'{"version":"draft-02","generatedAt":null,"tracks":[{"name":"a","namespace":null,"bitrate":null}]}',
	);

	assertEquals(catalog, { version: "draft-02", tracks: [{ name: "a" }] });
});

Deno.test("parseCatalog keeps a null unknown field as an extra field", () => {
	const track = parseTrack({ name: "a", "x-vendor": null });

	assertEquals(track.extraFields, { "x-vendor": null });
});

Deno.test("parseCatalog keeps unknown root fields, defaultNamespace among them", () => {
	const catalog = parseCatalog('{"version":"draft-02","tracks":[],"defaultNamespace":"ns"}');

	assertEquals([catalog.defaultNamespace, catalog.extraFields], [undefined, {
		defaultNamespace: "ns",
	}]);
});

Deno.test("parseCatalog reads a missing version as empty", () => {
	assertEquals(parseCatalog('{"tracks":[]}').version, "");
});

// ─── validateCatalog: track rules ────────────────────────────────────────────

Deno.test("validateCatalog reports the track-level rules of draft-02", async (t) => {
	const cases: { name: string; track: Track; problems: string[] }[] = [
		{
			name: "empty track",
			track: {},
			problems: [
				"tracks[0]: name is required and must not be empty",
				"tracks[0]: packaging is required",
				"tracks[0]: isLive is required",
			],
		},
		{
			name: "empty name counts as absent",
			track: { name: "", packaging: "cmaf", isLive: true },
			problems: ["tracks[0]: name is required and must not be empty"],
		},
		{
			name: "NUL in name",
			track: { name: "a\u0000b", packaging: "cmaf", isLive: true },
			problems: ['tracks[0]: name: "a\\u0000b" contains a NUL character'],
		},
		{
			name: "lone surrogate in name",
			track: { name: "a\ud800", packaging: "cmaf", isLive: true },
			problems: ['tracks[0]: name: "a\\ud800" is not valid UTF-8'],
		},
		{
			name: "namespace with an empty field",
			track: { namespace: "a//b", name: "n", packaging: "cmaf", isLive: true },
			problems: ['tracks[0]: namespace "a//b" contains an empty field'],
		},
		{
			name: "namespace with 33 fields",
			track: {
				namespace: Array(33).fill("x").join("/"),
				name: "n",
				packaging: "cmaf",
				isLive: true,
			},
			problems: [
				`tracks[0]: namespace "${
					Array(33).fill("x").join("/")
				}" has 33 fields; at most 32 are allowed`,
			],
		},
		{
			name: "trackDuration on a live track",
			track: { name: "n", packaging: "cmaf", isLive: true, trackDuration: 1 },
			problems: ["tracks[0]: trackDuration must not be present when isLive is true"],
		},
		{
			name: "targetLatency with buffers",
			track: {
				name: "n",
				packaging: "cmaf",
				isLive: true,
				targetLatency: 1,
				buffers: { target: 1 },
			},
			problems: ["tracks[0]: targetLatency and buffers must not both be present"],
		},
		{
			name: "eventtimeline without eventType",
			track: { name: "n", packaging: "eventtimeline", isLive: true },
			problems: ["tracks[0]: eventType is required for eventtimeline tracks"],
		},
		{
			name: "eventtimeline needs neither depends nor mimeType",
			track: { name: "n", packaging: "eventtimeline", isLive: true, eventType: "e" },
			problems: [],
		},
		{
			name: "eventType outside eventtimeline",
			track: { name: "n", packaging: "cmaf", isLive: true, eventType: "e" },
			problems: ["tracks[0]: eventType must only be set for eventtimeline tracks"],
		},
		{
			name: "loc video without its required fields",
			track: { name: "n", packaging: "loc", isLive: true, role: "video" },
			problems: [
				"tracks[0]: codec is required for loc tracks with role video",
				"tracks[0]: bitrate is required for loc tracks with role video",
				"tracks[0]: framerate is required for loc tracks with role video",
			],
		},
		{
			name: "loc audio without channelConfig",
			track: {
				name: "n",
				packaging: "loc",
				isLive: true,
				role: "audio",
				codec: "opus",
				bitrate: 1,
				samplerate: 48000,
			},
			problems: ["tracks[0]: channelConfig is required for loc tracks with role audio"],
		},
		{
			name: "loc with a role that needs nothing more",
			track: { name: "n", packaging: "loc", isLive: true, role: "caption" },
			problems: [],
		},
		{
			name: "mediatimeline without depends, with a template",
			track: {
				name: "n",
				packaging: "mediatimeline",
				isLive: true,
				template: {
					startMediaTime: 0,
					deltaMediaTime: 1,
					startLocation: { groupId: 0, objectId: 0 },
					deltaLocation: { groupId: 1, objectId: 0 },
					startWallclock: 0,
					deltaWallclock: 1,
				},
			},
			problems: [
				"tracks[0]: mediatimeline tracks must declare depends",
				"tracks[0]: mediatimeline tracks must not also carry a template",
			],
		},
		{
			name: "moqlog in tracks with the wrong role and name",
			track: { name: "log", packaging: "moqlog", isLive: true, role: "metrics" },
			problems: [
				"tracks[0]: moqlog tracks must be declared in publishTracks",
				"tracks[0]: moqlog tracks must have role log",
				'tracks[0]: moqlog track name must be a level from "0" to "7"',
			],
		},
		{
			name: "moq-secure-objects without cipherSuite",
			track: {
				name: "n",
				packaging: "cmaf",
				isLive: true,
				encryptionScheme: "moq-secure-objects",
			},
			problems: [
				"tracks[0]: cipherSuite is required when encryptionScheme is moq-secure-objects",
			],
		},
		{
			name: "another encryption scheme without cipherSuite",
			track: { name: "n", packaging: "cmaf", isLive: true, encryptionScheme: "other" },
			problems: [],
		},
		{
			name: "parent fields outside a clone or update",
			track: {
				name: "n",
				packaging: "cmaf",
				isLive: true,
				extraFields: { parentName: "p", parentNamespace: "q" },
			},
			problems: [
				"tracks[0]: parentName must only appear in a clone or update operation",
				"tracks[0]: parentNamespace must only appear in a clone or update operation",
			],
		},
		{
			name: "empty depends entry",
			track: { name: "n", packaging: "cmaf", isLive: true, depends: [""] },
			problems: ["tracks[0]: depends[0]: depends entry must not be an empty track name"],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(catalogOf([c.track])), c.problems);
		});
	}
});

Deno.test("validateCatalog checks packagingMetadata syntax", async (t) => {
	const cases: { metadata: string; packaging?: string; problem?: string }[] = [
		{ metadata: "" },
		{ metadata: "streamMapping=object" },
		{ metadata: "a=1;b=" },
		{ metadata: "a=1;", problem: 'packagingMetadata entry "" must be a key=value pair' },
		{ metadata: ";a=1", problem: 'packagingMetadata entry "" must be a key=value pair' },
		{ metadata: "a", problem: 'packagingMetadata entry "a" must be a key=value pair' },
		{ metadata: "=1", problem: 'packagingMetadata entry "=1" must be a key=value pair' },
		{
			metadata: "a=1=2",
			problem: `packagingMetadata entry "a=1=2" must not contain whitespace or a second '='`,
		},
		{
			metadata: "a= 1",
			problem: `packagingMetadata entry "a= 1" must not contain whitespace or a second '='`,
		},
		{
			metadata: "a= 1",
			problem: `packagingMetadata entry "a= 1" must not contain whitespace or a second '='`,
		},
		{
			metadata: "streamMapping=track",
			problem: 'packagingMetadata streamMapping "track" must be "group" or "object"',
		},
		{ metadata: "streamMapping=track", packaging: "cmaf" },
	];
	for (const c of cases) {
		await t.step(`${JSON.stringify(c.metadata)} on ${c.packaging ?? "loc"}`, () => {
			const track: Track = {
				name: "n",
				packaging: c.packaging ?? "loc",
				isLive: true,
				packagingMetadata: c.metadata,
			};

			assertEquals(
				problemsOf(catalogOf([track])),
				c.problem ? [`tracks[0]: ${c.problem}`] : [],
			);
		});
	}
});

// ─── validateCatalog: catalog rules ──────────────────────────────────────────

Deno.test("validateCatalog checks the version", async (t) => {
	const cases: { version: string; problems: string[] }[] = [
		{ version: "draft-02", problems: [] },
		{ version: "", problems: ["catalog version is required"] },
		{
			version: "draft-01",
			problems: ['unsupported catalog version "draft-01"; want "draft-02"'],
		},
	];
	for (const c of cases) {
		await t.step(JSON.stringify(c.version), () => {
			assertEquals(problemsOf({ version: c.version, tracks: [] }), c.problems);
		});
	}
});

Deno.test("validateCatalog accepts log and metrics tracks in publishTracks", () => {
	const catalog = catalogOf([], {
		publishTracks: [
			{ name: "3", packaging: "moqlog", role: "log", isLive: true },
			{ name: "0", packaging: "moqmetrics", role: "metrics", isLive: true },
		],
	});

	assertEquals(problemsOf(catalog), []);
});

Deno.test("validateCatalog requires unique Full Track Names across tracks and publishTracks", async (t) => {
	const cases: { name: string; catalog: Catalog; problems: string[] }[] = [
		{
			name: "inherited namespace equals the declared one",
			catalog: catalogOf(
				[{ ...video, name: "v" }, { ...video, namespace: "demo", name: "v" }],
				{ defaultNamespace: "demo" },
			),
			problems: ['tracks[1]: duplicate track identity "demo/v"'],
		},
		{
			name: "same name in another namespace",
			catalog: catalogOf([{ ...video, name: "v" }, {
				...video,
				namespace: "other",
				name: "v",
			}], {
				defaultNamespace: "demo",
			}),
			problems: [],
		},
		{
			name: "a publish track repeats a track",
			catalog: catalogOf([{ ...video, name: "v" }], {
				publishTracks: [{ name: "v", packaging: "cmaf", isLive: true }],
			}),
			problems: ['publishTracks[0]: duplicate track identity "v"'],
		},
		{
			name: "names that would collide if joined with |",
			catalog: catalogOf([
				{ ...video, namespace: "a|b", name: "c" },
				{ ...video, namespace: "a", name: "b|c" },
			]),
			problems: [],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(c.catalog), c.problems);
		});
	}
});

Deno.test("validateCatalog requires every depends entry to resolve", async (t) => {
	const timeline = (...depends: (string | number)[]): Track => ({
		name: "timeline",
		packaging: "mediatimeline",
		isLive: true,
		depends,
	});
	const cases: { name: string; catalog: Catalog; problems: string[] }[] = [
		{ name: "track name", catalog: catalogOf([video, timeline("video")]), problems: [] },
		{ name: "render group", catalog: catalogOf([video, timeline(1)]), problems: [] },
		{
			name: "publish track depends on a media track",
			catalog: catalogOf([video], {
				publishTracks: [{
					name: "events",
					packaging: "eventtimeline",
					eventType: "e",
					isLive: true,
					depends: ["video"],
				}],
			}),
			problems: [],
		},
		{
			name: "unknown track name",
			catalog: catalogOf([video, timeline("audio")]),
			problems: ['tracks[1]: depends[0]: track name "audio" does not match any other track'],
		},
		{
			name: "self reference by name",
			catalog: catalogOf([video, timeline("timeline")]),
			problems: [
				'tracks[1]: depends[0]: track name "timeline" does not match any other track',
			],
		},
		{
			// As in the Go package: the track's own renderGroup resolves it.
			name: "self reference by render group",
			catalog: catalogOf([{ ...timeline(7), renderGroup: 7 }]),
			problems: [],
		},
		{
			name: "ambiguous track name",
			catalog: catalogOf([video, { ...video, namespace: "other" }, timeline("video")]),
			problems: [
				'tracks[2]: depends[0]: track name "video" matches 2 tracks; it must match exactly one',
			],
		},
		{
			name: "unknown render group",
			catalog: catalogOf([video, timeline(2)]),
			problems: ["tracks[1]: depends[0]: renderGroup 2 does not match any track"],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(c.catalog), c.problems);
		});
	}
});

Deno.test("validateCatalog requires one accessibleWindowTime per altGroup", async (t) => {
	const track = (name: string, altGroup: number, window?: number): Track => ({
		name,
		packaging: "cmaf",
		isLive: true,
		altGroup,
		...(window === undefined ? {} : { accessibleWindowTime: window }),
	});
	const cases: { name: string; tracks: Track[]; problems: string[] }[] = [
		{ name: "same window", tracks: [track("a", 1, 30), track("b", 1, 30)], problems: [] },
		{ name: "different groups", tracks: [track("a", 1, 30), track("b", 2, 60)], problems: [] },
		{ name: "both absent", tracks: [track("a", 1), track("b", 1)], problems: [] },
		{
			name: "one absent",
			tracks: [track("a", 1), track("b", 1, 60)],
			problems: ["tracks[1]: accessibleWindowTime must match the other tracks of altGroup 1"],
		},
		{
			name: "third track differs",
			tracks: [track("a", 1, 1), track("b", 1, 1), track("c", 1, 2)],
			problems: ["tracks[2]: accessibleWindowTime must match the other tracks of altGroup 1"],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(catalogOf(c.tracks)), c.problems);
		});
	}
});

Deno.test("validateCatalog checks the Initialization Data List", async (t) => {
	const cases: { name: string; catalog: Catalog; problems: string[] }[] = [
		{
			name: "every type",
			catalog: catalogOf([], {
				initDataList: [
					{ id: "a", type: "inline", data: "AAAA" },
					{ id: "b", type: "track-property", data: "0x0D" },
					{ id: "c", type: "object-property", data: "0F" },
					{ id: "d" },
				],
			}),
			problems: [],
		},
		{
			name: "property type not hex",
			catalog: catalogOf([], {
				initDataList: [{ id: "a", type: "track-property", data: "video" }],
			}),
			problems: ['initDataList[0]: data "video" must be a property type in hex notation'],
		},
		{
			name: "unknown type",
			catalog: catalogOf([], { initDataList: [{ id: "a", type: "url" }] }),
			problems: [
				'initDataList[0]: type "url" must be "inline", "track-property" or "object-property"',
			],
		},
		{
			name: "missing id",
			catalog: catalogOf([], { initDataList: [{ id: "" }] }),
			problems: ["initDataList[0]: id is required"],
		},
		{
			name: "duplicate id",
			catalog: catalogOf([], { initDataList: [{ id: "a" }, { id: "a" }] }),
			problems: ['initDataList[1]: duplicate init id "a"'],
		},
		{
			name: "dangling initRef without any initDataList",
			catalog: catalogOf([{ ...video, initRef: "missing" }]),
			problems: ['tracks[0]: initRef "missing" does not match any initDataList id'],
		},
		{
			name: "dangling initRef on a publish track",
			catalog: catalogOf([], {
				initDataList: [{ id: "a" }],
				publishTracks: [{ name: "p", packaging: "cmaf", isLive: true, initRef: "b" }],
			}),
			problems: ['publishTracks[0]: initRef "b" does not match any initDataList id'],
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertEquals(problemsOf(c.catalog), c.problems);
		});
	}
});

Deno.test("initDataPropertyType reads the hex property type of a property entry", async (t) => {
	const cases: { data: string; type?: string; expected?: bigint }[] = [
		{ data: "0x0D", expected: 13n },
		{ data: "0F", type: "object-property", expected: 15n },
		{ data: "ffffffffffffffff", expected: 18446744073709551615n },
		{ data: "10000000000000000" },
		{ data: "0x" },
		{ data: "-1" },
		{ data: "0x0D", type: "inline" },
	];
	for (const c of cases) {
		await t.step(`${c.type ?? "track-property"} ${c.data}`, () => {
			const ref = { id: "i", type: c.type ?? "track-property", data: c.data };

			if (c.expected === undefined) {
				assertThrows(
					() => initDataPropertyType(ref),
					Error,
					"does not name a property type",
				);
			} else {
				assertEquals(initDataPropertyType(ref), c.expected);
			}
		});
	}
});

// ─── stringifyCatalog ────────────────────────────────────────────────────────

Deno.test("stringifyCatalog writes the draft-02 shape", () => {
	const catalog: Catalog = {
		defaultNamespace: "not/written",
		version: "draft-02",
		generatedAt: 10,
		isComplete: true,
		tracks: [{ name: "clip", packaging: "cmaf", label: "", extraFields: { extra: 1 } }],
		extraFields: { hello: "world", initDataList: "stale" },
		initDataList: [{ id: "i", type: "inline", data: "AA==" }],
	};

	assertEquals(
		stringifyCatalog(catalog),
		JSON.stringify({
			hello: "world",
			version: "draft-02",
			generatedAt: 10,
			isComplete: true,
			tracks: [{ extra: 1, name: "clip", packaging: "cmaf" }],
			initDataList: [{ id: "i", type: "inline", data: "AA==" }],
		}),
	);
});

Deno.test("stringifyCatalog round-trips every track field", () => {
	const track: Track = {
		namespace: "a/b",
		name: "n",
		packaging: "loc",
		packagingMetadata: "streamMapping=group",
		role: "video",
		isLive: false,
		buffers: { target: 1, min: 0, max: 2, extraFields: { x: true } },
		label: "L",
		renderGroup: 0,
		altGroup: 0,
		initRef: "i",
		depends: ["other", 0],
		template: {
			startMediaTime: 0,
			deltaMediaTime: 1,
			startLocation: { groupId: 0, objectId: 0 },
			deltaLocation: { groupId: 1, objectId: 0 },
			startWallclock: 0,
			deltaWallclock: 1,
		},
		temporalId: 0,
		spatialId: 0,
		codec: "c",
		mimeType: "m",
		framerate: 30,
		timescale: 90000,
		bitrate: 1,
		avgBitrate: 1,
		maxGopDuration: 1,
		maxGroupDuration: 1,
		width: 1,
		height: 1,
		samplerate: 1,
		channelConfig: "2",
		displayWidth: 1,
		displayHeight: 1,
		lang: "en",
		trackDuration: 1,
		connectionURI: "moqt://x",
		encryptionScheme: "moq-secure-objects",
		cipherSuite: "s",
		keyId: "k",
		trackBaseKey: "b",
		authInfo: { cat: { nested: [1] } },
		accessibility: [{ scheme: "s", value: "v", extraFields: { y: 1 } }],
		accessibleWindowTime: 1,
		keyframeSpacing: 1,
		extraFields: { "x-vendor": "v" },
	};

	const parsed = parseCatalog(stringifyCatalog(catalogOf([track])));

	assertEquals(parsed.tracks, [track]);
});

// ─── identity helpers ────────────────────────────────────────────────────────

Deno.test("effectiveNamespace falls back to the default, then the inherited marker", () => {
	assertEquals(
		[
			effectiveNamespace({ namespace: "ns" }, "fallback"),
			effectiveNamespace({}, "fallback"),
			effectiveNamespace({}, undefined),
		],
		["ns", "fallback", "\u0000catalog"],
	);
});

Deno.test("trackId joins the effective namespace and the name", () => {
	assertEquals(trackId({ name: "v" }, "fallback"), "fallback|v");
});
