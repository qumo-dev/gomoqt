import { assertEquals, assertNotEquals, assertThrows } from "@std/assert";
import {
	applyCatalogDelta,
	type Catalog,
	type CatalogDelta,
	parseCatalog,
	parseCatalogDelta,
	stringifyCatalog,
	stringifyCatalogDelta,
	validateCatalog,
	ValidationError,
} from "./mod.ts";

// Emitted verbatim by the Go msf package (json.Marshal of msf.Catalog and
// msf.CatalogDelta at v0.19.0), so these tests pin interop with the Go
// implementation rather than this package's reading of draft-ietf-moq-msf-01.
const GO_CATALOG =
	'{"initDataList":[{"data":"AUIAH//hABdnQgAf","id":"video","type":"inline"},{"data":"EhA=","id":"audio","type":"inline"}],"tracks":[{"codec":"avc1.64001f","initRef":"video","isLive":true,"name":"video","packaging":"loc","role":"video"},{"codec":"mp4a.40.2","initRef":"audio","isLive":true,"name":"audio","packaging":"loc","role":"audio"}],"version":1}';
const GO_DELTA =
	'{"deltaUpdate":[{"op":"add","tracks":[{"codec":"avc1.640028","initRef":"video","isLive":true,"name":"video-hd","packaging":"loc","role":"video"}]},{"op":"remove","tracks":[{"name":"audio"}]},{"op":"clone","tracks":[{"codec":"avc1.64000d","name":"video-lo","parentName":"video","parentNamespace":"live/alice"}]}]}';

const VIDEO_INIT = "AUIAH//hABdnQgAf";
const AUDIO_INIT = "EhA=";

// Go keeps DefaultNamespace out of the wire form: in msf-01 it is implied by
// the catalog track itself, so a receiver supplies it. Parse, then set it as
// the Go catalog that produced GO_CATALOG had it.
function goBaseCatalog(): Catalog {
	const catalog = parseCatalog(GO_CATALOG);
	catalog.defaultNamespace = "live/alice";
	return catalog;
}

function validationProblems(fn: () => void): string[] {
	try {
		fn();
	} catch (err) {
		if (err instanceof ValidationError) {
			return err.problems;
		}
		throw err;
	}
	return [];
}

// ─── Go → TS: independent catalog ────────────────────────────────────────────

Deno.test("parseCatalog resolves each initRef of a Go-emitted catalog into initData", () => {
	const catalog = parseCatalog(GO_CATALOG);

	assertEquals(
		catalog.tracks.map((t) => [t.name, t.initRef, t.initData]),
		[["video", "video", VIDEO_INIT], ["audio", "audio", AUDIO_INIT]],
	);
});

Deno.test("parseCatalog keeps the initDataList as a typed field, not an extra field", () => {
	const catalog = parseCatalog(GO_CATALOG);

	assertEquals(catalog.initDataList, [
		{ id: "video", type: "inline", data: VIDEO_INIT, extraFields: undefined },
		{ id: "audio", type: "inline", data: AUDIO_INIT, extraFields: undefined },
	]);
	assertEquals(catalog.extraFields, undefined);
});

Deno.test("validateCatalog accepts a Go-emitted catalog", () => {
	const catalog = parseCatalog(GO_CATALOG);

	assertEquals(validationProblems(() => validateCatalog(catalog)), []);
});

Deno.test("stringifyCatalog round-trips a Go-emitted catalog to the same JSON content", () => {
	const catalog = parseCatalog(GO_CATALOG);

	const json = stringifyCatalog(catalog);

	assertEquals(JSON.parse(json), JSON.parse(GO_CATALOG));
});

// ─── Go → TS: delta ──────────────────────────────────────────────────────────

Deno.test("parseCatalogDelta parses every operation of a Go-emitted delta", () => {
	const delta = parseCatalogDelta(GO_DELTA);

	assertEquals(delta.deltaOpOrder, ["addTracks", "removeTracks", "cloneTracks"]);
	assertEquals(delta.addTracks.map((t) => [t.name, t.initRef]), [["video-hd", "video"]]);
	assertEquals(delta.removeTracks.map((r) => r.name), ["audio"]);
	assertEquals(
		delta.cloneTracks.map((c) => [c.track.name, c.parentName, c.parentNamespace]),
		[["video-lo", "video", "live/alice"]],
	);
});

Deno.test("applyCatalogDelta resolves an added track's initRef against the base catalog's list", () => {
	const base = goBaseCatalog();
	const delta = parseCatalogDelta(GO_DELTA);

	const result = applyCatalogDelta(base, delta);

	assertEquals(result.tracks.find((t) => t.name === "video-hd")?.initData, VIDEO_INIT);
});

// Expected tracks and clone codec are what Go's own Catalog.ApplyDelta returns
// for the same inputs (video, video-hd, video-lo; clone codec avc1.64000d,
// clone initRef inherited as "video").
Deno.test("applyCatalogDelta applies a Go-emitted delta's removes and clones", () => {
	const base = goBaseCatalog();
	const delta = parseCatalogDelta(GO_DELTA);

	const result = applyCatalogDelta(base, delta);

	assertEquals(result.tracks.map((t) => t.name), ["video", "video-hd", "video-lo"]);
	const clone = result.tracks.find((t) => t.name === "video-lo");
	assertEquals([clone?.codec, clone?.initData], ["avc1.64000d", VIDEO_INIT]);
});

// ─── TS → wire: catalog init data ────────────────────────────────────────────

Deno.test("stringifyCatalog writes a track's initData as an initDataList entry, never inline", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [{ name: "video", packaging: "loc", isLive: true, initData: VIDEO_INIT }],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.initDataList, [{ id: "video", type: "inline", data: VIDEO_INIT }]);
	assertEquals(wire.tracks, [{
		name: "video",
		packaging: "loc",
		isLive: true,
		initRef: "video",
	}]);
});

Deno.test("stringifyCatalog gives tracks that share a payload one shared entry", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [
			{ name: "hd", packaging: "loc", isLive: true, initData: VIDEO_INIT },
			{ name: "sd", packaging: "loc", isLive: true, initData: VIDEO_INIT },
		],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.initDataList, [{ id: "hd", type: "inline", data: VIDEO_INIT }]);
	assertEquals(wire.tracks.map((t: { initRef: string }) => t.initRef), ["hd", "hd"]);
});

Deno.test("stringifyCatalog does not let a stale initRef hide a payload edited in place", () => {
	const catalog = parseCatalog(GO_CATALOG);
	catalog.tracks[0]!.initData = "bmV3";

	const wire = JSON.parse(stringifyCatalog(catalog));

	const ref = wire.tracks[0].initRef;
	assertNotEquals(ref, "video");
	assertEquals(
		wire.initDataList.find((e: { id: string }) => e.id === ref),
		{ id: ref, type: "inline", data: "bmV3" },
	);
});

Deno.test("stringifyCatalog derives a fresh id when the track's name is already taken", () => {
	const catalog: Catalog = {
		version: 1,
		initDataList: [{ id: "video", type: "inline", data: VIDEO_INIT }],
		tracks: [{ name: "video", packaging: "loc", isLive: true, initData: "bmV3" }],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.tracks[0].initRef, "video-2");
});

Deno.test("stringifyCatalog names an unnamed track's entry init", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [{ name: "", packaging: "loc", isLive: true, initData: VIDEO_INIT }],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.tracks[0].initRef, "init");
});

Deno.test("stringifyCatalog spreads a track's extraFields instead of writing the key", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [{ name: "v", packaging: "cmaf", extraFields: { vendor: 1 } }],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.tracks, [{ vendor: 1, name: "v", packaging: "cmaf" }]);
});

// ─── validateCatalog: Initialization Data List rules ────────────────────────

Deno.test("validateCatalog rejects a malformed Initialization Data List", async (t) => {
	const cases: { name: string; catalog: Catalog; problem: string }[] = [
		{
			name: "duplicate id",
			catalog: {
				version: 1,
				initDataList: [{ id: "a", data: "AA==" }, { id: "a", data: "AQ==" }],
				tracks: [],
			},
			problem: 'initDataList[1]: duplicate init id "a"',
		},
		{
			name: "non-inline type",
			catalog: { version: 1, initDataList: [{ id: "a", type: "url" }], tracks: [] },
			problem: 'initDataList[0]: type must be "inline"',
		},
		{
			name: "missing id",
			catalog: { version: 1, initDataList: [{ id: "", data: "AA==" }], tracks: [] },
			problem: "initDataList[0]: id is required",
		},
		{
			name: "dangling initRef",
			catalog: {
				version: 1,
				tracks: [{ name: "v", packaging: "cmaf", initRef: "missing" }],
			},
			problem: 'tracks[0]: initRef "missing" does not match any initDataList id',
		},
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			const problems = validationProblems(() => validateCatalog(c.catalog));

			assertEquals(problems, [c.problem]);
		});
	}
});

Deno.test("validateCatalog accepts an initRef its track can supply itself", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [{ name: "v", packaging: "cmaf", initRef: "own", initData: "AA==" }],
	};

	assertEquals(validationProblems(() => validateCatalog(catalog)), []);
});

// ─── Delta wire form ─────────────────────────────────────────────────────────

Deno.test("parseCatalogDelta rejects an unknown operation", () => {
	assertThrows(
		() => parseCatalogDelta('{"deltaUpdate":[{"op":"rename","tracks":[]}]}'),
		Error,
		'unknown delta update op "rename"',
	);
});

Deno.test("parseCatalogDelta rejects a delta without a deltaUpdate array", () => {
	assertThrows(() => parseCatalogDelta('{"generatedAt":1}'), Error, "deltaUpdate");
});

Deno.test("parseCatalogDelta appends repeated operations and records first-seen order", () => {
	const delta = parseCatalogDelta(
		'{"deltaUpdate":[{"op":"remove","tracks":[{"name":"a"}]},{"op":"add","tracks":[{"name":"b","packaging":"cmaf"}]},{"op":"remove","tracks":[{"name":"c"}]}]}',
	);

	assertEquals(delta.removeTracks.map((r) => r.name), ["a", "c"]);
	assertEquals(delta.deltaOpOrder, ["removeTracks", "addTracks"]);
});

Deno.test("stringifyCatalogDelta refuses a track whose initData it cannot represent", () => {
	const delta: CatalogDelta = {
		addTracks: [{ name: "v", packaging: "cmaf", initData: VIDEO_INIT }],
		removeTracks: [],
		cloneTracks: [],
	};

	assertThrows(() => stringifyCatalogDelta(delta), Error, "addTracks[0] carries initData");
});

Deno.test("stringifyCatalogDelta writes an initRef and drops the initData it resolved to", () => {
	const delta: CatalogDelta = {
		addTracks: [{ name: "v", packaging: "cmaf", initRef: "video", initData: VIDEO_INIT }],
		removeTracks: [],
		cloneTracks: [],
	};

	const wire = JSON.parse(stringifyCatalogDelta(delta));

	assertEquals(wire.deltaUpdate, [
		{ op: "add", tracks: [{ name: "v", packaging: "cmaf", initRef: "video" }] },
	]);
});

Deno.test("stringifyCatalogDelta writes a clone's parentNamespace", () => {
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [{ track: { name: "lo" }, parentName: "hi", parentNamespace: "other" }],
	};

	const wire = JSON.parse(stringifyCatalogDelta(delta));

	assertEquals(wire.deltaUpdate, [
		{ op: "clone", tracks: [{ name: "lo", parentName: "hi", parentNamespace: "other" }] },
	]);
});

Deno.test("stringifyCatalogDelta round-trips a Go-emitted delta to the same JSON content", () => {
	const delta = parseCatalogDelta(GO_DELTA);

	const json = stringifyCatalogDelta(delta);

	assertEquals(JSON.parse(json), JSON.parse(GO_DELTA));
});

Deno.test("applyCatalogDelta looks for a clone's parent in parentNamespace, not the clone's own", () => {
	const base: Catalog = {
		version: 1,
		defaultNamespace: "home",
		tracks: [{ namespace: "other", name: "src", packaging: "cmaf", codec: "c1" }],
	};
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [{
			track: { namespace: "home", name: "copy" },
			parentName: "src",
			parentNamespace: "other",
		}],
	};

	const result = applyCatalogDelta(base, delta);

	assertEquals(result.tracks.find((t) => t.name === "copy")?.codec, "c1");
});

Deno.test("applyCatalogDelta resolves a clone's overridden initRef instead of inheriting the parent's payload", () => {
	const base = goBaseCatalog();
	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [{ track: { name: "video-alt", initRef: "audio" }, parentName: "video" }],
	};

	const result = applyCatalogDelta(base, delta);

	const clone = result.tracks.find((t) => t.name === "video-alt");
	assertEquals([clone?.initRef, clone?.initData], ["audio", AUDIO_INIT]);
});

Deno.test("stringifyCatalog treats an empty initRef as unset", () => {
	const catalog: Catalog = {
		version: 1,
		tracks: [{ name: "video", packaging: "loc", initRef: "", initData: VIDEO_INIT }],
	};

	const wire = JSON.parse(stringifyCatalog(catalog));

	assertEquals(wire.initDataList, [{ id: "video", type: "inline", data: VIDEO_INIT }]);
	assertEquals(wire.tracks[0].initRef, "video");
});
