import { assertEquals, assertThrows } from "@std/assert";
import {
	applyCatalogDelta,
	type Catalog,
	type CatalogDelta,
	parseCatalog,
	parseCatalogDelta,
	stringifyCatalog,
	stringifyCatalogDelta,
	validateCatalog,
	validateCatalogDelta,
} from "./mod.ts";

// Emitted verbatim by the Go msf package on this branch (MarshalJSON of
// msf.Catalog and msf.CatalogDelta, and of the catalog Catalog.ApplyDelta
// returns for the two, with DefaultNamespace "live/alice"), so these tests
// pin interop with the Go implementation of draft-ietf-moq-msf-02 rather
// than this package's reading of it. The delta interleaves its operations
// (add, remove, clone, update, add, empty update), removes and re-adds
// "audio", and the catalog uses string and number depends.
const GO_CATALOG =
	'{"generatedAt":1746104606044,"publishTracks":[{"connectionURI":"moqt://relay.example.com/logs","isLive":true,"name":"3","packaging":"moqlog","role":"log"}],"tracks":[{"accessibility":[{"scheme":"urn:scte:dash:cc:cea-608:2015","value":"CC1=eng"}],"accessibleWindowTime":30000,"altGroup":1,"authInfo":{"cat":"abc"},"avgBitrate":4000000,"bitrate":5000000,"buffers":{"min":100,"target":500,"x-b":1},"cipherSuite":"aes-128-gcm-sha256","codec":"av01.0.08M.10.0.110.09","encryptionScheme":"moq-secure-objects","framerate":30,"height":1080,"initRef":"video-init","isLive":true,"keyId":"k1","keyframeSpacing":60,"maxGopDuration":2000,"maxGroupDuration":2000,"name":"video","packaging":"loc","packagingMetadata":"streamMapping=object;profile=main","renderGroup":1,"role":"video","trackBaseKey":"AAEC","width":1920,"x-vendor":[1,2]},{"accessibleWindowTime":30000,"altGroup":1,"bitrate":800000,"codec":"av01.0.04M.10.0.110.09","framerate":30,"height":360,"isLive":true,"name":"video-lo","packaging":"loc","renderGroup":1,"role":"video","template":[0,2000,[0,0],[1,0],1746104600000,2000],"width":640},{"bitrate":32000,"channelConfig":"2","codec":"opus","initRef":"audio-init","isLive":true,"lang":"en","name":"audio","packaging":"loc","renderGroup":1,"role":"audio","samplerate":48000},{"depends":["video",1],"isLive":true,"mimeType":"application/json","name":"timeline","packaging":"mediatimeline","role":"mediatimeline"},{"depends":[1],"eventType":"com.example.score","isLive":true,"mimeType":"application/json","name":"events","packaging":"eventtimeline","role":"eventtimeline"},{"isLive":true,"name":"catalog","namespace":"live/alice/extras","packaging":"catalog"}],"version":"draft-02","x-root":{"note":"kept"},"initDataList":[{"data":"AUIAH//hABdnQgAf","id":"video-init","type":"inline"},{"data":"0x2a","id":"audio-init","type":"track-property"}]}';
const GO_DELTA =
	'{"deltaUpdate":[{"op":"add","tracks":[{"accessibleWindowTime":30000,"altGroup":1,"bitrate":8000000,"codec":"av01.0.12M.10.0.110.09","framerate":60,"initRef":"video-init","isLive":true,"name":"video-hd","packaging":"loc","renderGroup":1,"role":"video"}]},{"op":"remove","tracks":[{"name":"audio"}]},{"op":"clone","tracks":[{"bitrate":1500000,"codec":"av01.0.05M.10.0.110.09","height":720,"name":"video-sd","parentName":"video","width":1280}]},{"op":"update","tracks":[{"bitrate":400000,"label":"Low","parentName":"video-lo"}]},{"op":"add","tracks":[{"bitrate":64000,"channelConfig":"2","codec":"opus","depends":["video"],"isLive":true,"name":"audio","packaging":"loc","renderGroup":1,"role":"audio","samplerate":48000}]},{"op":"update","tracks":[]}],"generatedAt":1746104607000,"x-delta":true}';
const GO_RESULT =
	'{"generatedAt":1746104607000,"publishTracks":[{"connectionURI":"moqt://relay.example.com/logs","isLive":true,"name":"3","packaging":"moqlog","role":"log"}],"tracks":[{"accessibility":[{"scheme":"urn:scte:dash:cc:cea-608:2015","value":"CC1=eng"}],"accessibleWindowTime":30000,"altGroup":1,"authInfo":{"cat":"abc"},"avgBitrate":4000000,"bitrate":5000000,"buffers":{"min":100,"target":500,"x-b":1},"cipherSuite":"aes-128-gcm-sha256","codec":"av01.0.08M.10.0.110.09","encryptionScheme":"moq-secure-objects","framerate":30,"height":1080,"initRef":"video-init","isLive":true,"keyId":"k1","keyframeSpacing":60,"maxGopDuration":2000,"maxGroupDuration":2000,"name":"video","packaging":"loc","packagingMetadata":"streamMapping=object;profile=main","renderGroup":1,"role":"video","trackBaseKey":"AAEC","width":1920,"x-vendor":[1,2]},{"accessibleWindowTime":30000,"altGroup":1,"bitrate":400000,"codec":"av01.0.04M.10.0.110.09","framerate":30,"height":360,"isLive":true,"label":"Low","name":"video-lo","packaging":"loc","renderGroup":1,"role":"video","template":[0,2000,[0,0],[1,0],1746104600000,2000],"width":640},{"depends":["video",1],"isLive":true,"mimeType":"application/json","name":"timeline","packaging":"mediatimeline","role":"mediatimeline"},{"depends":[1],"eventType":"com.example.score","isLive":true,"mimeType":"application/json","name":"events","packaging":"eventtimeline","role":"eventtimeline"},{"isLive":true,"name":"catalog","namespace":"live/alice/extras","packaging":"catalog"},{"accessibleWindowTime":30000,"altGroup":1,"bitrate":8000000,"codec":"av01.0.12M.10.0.110.09","framerate":60,"initRef":"video-init","isLive":true,"name":"video-hd","packaging":"loc","renderGroup":1,"role":"video"},{"accessibility":[{"scheme":"urn:scte:dash:cc:cea-608:2015","value":"CC1=eng"}],"accessibleWindowTime":30000,"altGroup":1,"authInfo":{"cat":"abc"},"avgBitrate":4000000,"bitrate":1500000,"buffers":{"min":100,"target":500,"x-b":1},"cipherSuite":"aes-128-gcm-sha256","codec":"av01.0.05M.10.0.110.09","encryptionScheme":"moq-secure-objects","framerate":30,"height":720,"initRef":"video-init","isLive":true,"keyId":"k1","keyframeSpacing":60,"maxGopDuration":2000,"maxGroupDuration":2000,"name":"video-sd","packaging":"loc","packagingMetadata":"streamMapping=object;profile=main","renderGroup":1,"role":"video","trackBaseKey":"AAEC","width":1280,"x-vendor":[1,2]},{"bitrate":64000,"channelConfig":"2","codec":"opus","depends":["video"],"isLive":true,"name":"audio","packaging":"loc","renderGroup":1,"role":"audio","samplerate":48000}],"version":"draft-02","x-delta":true,"x-root":{"note":"kept"},"initDataList":[{"data":"AUIAH//hABdnQgAf","id":"video-init","type":"inline"},{"data":"0x2a","id":"audio-init","type":"track-property"}]}';

// Go keeps DefaultNamespace out of the wire form: it is implied by the
// catalog track itself, so a receiver supplies it.
function goBaseCatalog(): Catalog {
	const catalog = parseCatalog(GO_CATALOG);
	catalog.defaultNamespace = "live/alice";
	return catalog;
}

// ─── Go → TS ─────────────────────────────────────────────────────────────────

Deno.test("validateCatalog accepts a Go-emitted draft-02 catalog", () => {
	const catalog = goBaseCatalog();

	validateCatalog(catalog);
});

Deno.test("parseCatalog reads every draft-02 field of a Go-emitted catalog", () => {
	const catalog = parseCatalog(GO_CATALOG);

	assertEquals(catalog.version, "draft-02");
	assertEquals(catalog.tracks[0]?.buffers, { target: 500, min: 100, extraFields: { "x-b": 1 } });
	assertEquals(catalog.tracks[1]?.template, {
		startMediaTime: 0,
		deltaMediaTime: 2000,
		startLocation: { groupId: 0, objectId: 0 },
		deltaLocation: { groupId: 1, objectId: 0 },
		startWallclock: 1746104600000,
		deltaWallclock: 2000,
	});
	assertEquals(catalog.tracks[3]?.depends, ["video", 1]);
	assertEquals(catalog.publishTracks?.[0]?.connectionURI, "moqt://relay.example.com/logs");
	assertEquals(catalog.initDataList?.[1], {
		id: "audio-init",
		type: "track-property",
		data: "0x2a",
	});
	assertEquals(catalog.extraFields, { "x-root": { note: "kept" } });
});

Deno.test("parseCatalogDelta records each operation of a Go-emitted delta in wire order", () => {
	const delta = parseCatalogDelta(GO_DELTA);

	assertEquals(delta.deltaOps, [
		{ kind: "addTracks", count: 1 },
		{ kind: "removeTracks", count: 1 },
		{ kind: "cloneTracks", count: 1 },
		{ kind: "updateTracks", count: 1 },
		{ kind: "addTracks", count: 1 },
		{ kind: "updateTracks", count: 0 },
	]);
});

Deno.test("validateCatalogDelta accepts a Go-emitted delta", () => {
	const delta = parseCatalogDelta(GO_DELTA);

	validateCatalogDelta(delta);
});

Deno.test("applyCatalogDelta gives the catalog Go's ApplyDelta gives", () => {
	const delta = parseCatalogDelta(GO_DELTA);

	const result = applyCatalogDelta(goBaseCatalog(), delta);

	assertEquals(JSON.parse(stringifyCatalog(result)), JSON.parse(GO_RESULT));
});

// ─── TS → Go: re-encoding gives the document Go emitted ──────────────────────

Deno.test("stringifyCatalog re-encodes a Go-emitted catalog to the same JSON content", () => {
	const json = stringifyCatalog(parseCatalog(GO_CATALOG));

	assertEquals(JSON.parse(json), JSON.parse(GO_CATALOG));
});

Deno.test("stringifyCatalog writes initDataList after tracks, as Go does", () => {
	const keys = Object.keys(JSON.parse(stringifyCatalog(parseCatalog(GO_CATALOG))));

	assertEquals(keys.at(-1), "initDataList");
});

Deno.test("stringifyCatalogDelta re-encodes a Go-emitted delta to the same JSON content", () => {
	const json = stringifyCatalogDelta(parseCatalogDelta(GO_DELTA));

	assertEquals(JSON.parse(json), JSON.parse(GO_DELTA));
});

Deno.test("stringifyCatalog writes an empty catalog's tracks as [], as Go does", () => {
	const catalog: Catalog = { version: "draft-02", tracks: [] };

	assertEquals(stringifyCatalog(catalog), '{"version":"draft-02","tracks":[]}');
});

Deno.test("stringifyCatalogDelta writes an empty operation's tracks as [], as Go does", () => {
	const delta: CatalogDelta = {
		generatedAt: 1,
		addTracks: [],
		removeTracks: [{ name: "a" }],
		cloneTracks: [],
		updateTracks: [],
		deltaOps: [{ kind: "removeTracks", count: 1 }, { kind: "updateTracks", count: 0 }],
	};

	assertEquals(
		JSON.parse(stringifyCatalogDelta(delta)).deltaUpdate,
		[{ op: "remove", tracks: [{ name: "a" }] }, { op: "update", tracks: [] }],
	);
});

Deno.test("parseCatalog rejects a version Go does not accept", () => {
	const catalog = parseCatalog('{"version":"draft-01","tracks":[]}');

	assertThrows(() => validateCatalog(catalog), Error, 'unsupported catalog version "draft-01"');
});
