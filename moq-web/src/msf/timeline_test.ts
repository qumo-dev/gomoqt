import { assertEquals, assertThrows } from "@std/assert";
import {
	decodeLocation,
	decodeMediaTimelineEntry,
	encodeEventTimelineRecord,
	encodeLocation,
	encodeMediaTimelineEntry,
	parseCatalog,
	parseEventTimelineRecord,
	type Template,
	templateEntry,
	validateEventTimelineRecord,
	validateMediaTimeline,
} from "./mod.ts";

// ─── decodeLocation ──────────────────────────────────────────────────────────

Deno.test("decodeLocation rejects non-array", () => {
	assertThrows(() => decodeLocation("not-an-array"), Error, "exactly 2 items");
});

Deno.test("decodeLocation rejects wrong length", () => {
	assertThrows(() => decodeLocation([1, 2, 3]), Error, "exactly 2 items");
});

Deno.test("decodeLocation rejects non-numeric items", () => {
	assertThrows(() => decodeLocation([1, "x"]), Error, "must be numbers");
});

Deno.test("decodeLocation returns Location for valid input", () => {
	assertEquals(decodeLocation([5, 9]), { groupId: 5, objectId: 9 });
	assertEquals(decodeLocation([0, 0]), { groupId: 0, objectId: 0 });
});

// ─── encodeLocation ──────────────────────────────────────────────────────────

Deno.test("encodeLocation returns tuple", () => {
	assertEquals(encodeLocation({ groupId: 3, objectId: 7 }), [3, 7]);
});

// ─── decodeMediaTimelineEntry ─────────────────────────────────────────────────

Deno.test("decodeMediaTimelineEntry rejects invalid shape", () => {
	assertThrows(
		() => decodeMediaTimelineEntry("not-an-array"),
		Error,
		"mediaTime",
	);
});

Deno.test("decodeMediaTimelineEntry rejects wrong element types", () => {
	assertThrows(
		() => decodeMediaTimelineEntry([1, [0, "bad"], 2]),
		Error,
		"mediaTime",
	);
});

Deno.test("decodeMediaTimelineEntry returns MediaTimelineEntry for valid input", () => {
	const entry = decodeMediaTimelineEntry([100, [5, 9], 200]);
	assertEquals(entry.mediaTime, 100);
	assertEquals(entry.location, { groupId: 5, objectId: 9 });
	assertEquals(entry.wallclock, 200);
});

// ─── encodeMediaTimelineEntry ─────────────────────────────────────────────────

Deno.test("encodeMediaTimelineEntry returns tuple", () => {
	const encoded = encodeMediaTimelineEntry({
		mediaTime: 100,
		location: { groupId: 5, objectId: 9 },
		wallclock: 200,
	});
	assertEquals(encoded, [100, [5, 9], 200]);
});

// ─── validateMediaTimeline ───────────────────────────────────────────────────

Deno.test("validateMediaTimeline accepts entries ordered by media time", () => {
	validateMediaTimeline([
		{ mediaTime: 0, location: { groupId: 9, objectId: 0 }, wallclock: 0 },
		{ mediaTime: 0, location: { groupId: 3, objectId: 0 }, wallclock: 0 },
		{ mediaTime: 10, location: { groupId: 1, objectId: 0 }, wallclock: 0 },
	]);
});

Deno.test("validateMediaTimeline rejects an entry earlier than the one before it", () => {
	assertThrows(
		() =>
			validateMediaTimeline([
				{ mediaTime: 10, location: { groupId: 0, objectId: 0 }, wallclock: 0 },
				{ mediaTime: 5, location: { groupId: 1, objectId: 0 }, wallclock: 0 },
			]),
		Error,
		"entry 1 has media time 5 before the previous entry's 10",
	);
});

// ─── templates ───────────────────────────────────────────────────────────────

const template: Template = {
	startMediaTime: 1000,
	deltaMediaTime: 2000,
	startLocation: { groupId: 5, objectId: 0 },
	deltaLocation: { groupId: 1, objectId: 0 },
	startWallclock: 1746104600000,
	deltaWallclock: 2000,
};

Deno.test("templateEntry computes the n-th entry of a template", () => {
	assertEquals(templateEntry(template, 3), {
		mediaTime: 7000,
		location: { groupId: 8, objectId: 0 },
		wallclock: 1746104606000,
	});
});

Deno.test("templateEntry rejects a negative or fractional index", async (t) => {
	for (const n of [-1, 1.5]) {
		await t.step(String(n), () => {
			assertThrows(() => templateEntry(template, n), RangeError);
		});
	}
});

Deno.test("a track's template is read from its six-element array form", () => {
	const catalog = parseCatalog(
		'{"tracks":[{"name":"v","template":[1000,2000,[5,0],[1,0],1746104600000,2000]}]}',
	);

	assertEquals(catalog.tracks[0]?.template, template);
});

// ─── validateEventTimelineRecord ─────────────────────────────────────────────

Deno.test("validateEventTimelineRecord requires exactly one index", async (t) => {
	const cases = [
		{ name: "none", record: { data: "x" } },
		{ name: "two", record: { wallclock: 1, location: { groupId: 0, objectId: 0 }, data: "x" } },
	];
	for (const c of cases) {
		await t.step(c.name, () => {
			assertThrows(
				() => validateEventTimelineRecord(c.record),
				Error,
				"exactly one of T, L, or M",
			);
		});
	}
});

Deno.test("validateEventTimelineRecord rejects record missing data", () => {
	assertThrows(() => validateEventTimelineRecord({ wallclock: 1 }), Error, "must contain data");
});

Deno.test("validateEventTimelineRecord accepts a null data payload", () => {
	validateEventTimelineRecord({ mediaTime: 500, data: null });
});

// ─── parseEventTimelineRecord ─────────────────────────────────────────────────

Deno.test("parseEventTimelineRecord rejects non-object", () => {
	assertThrows(
		() => parseEventTimelineRecord("not-an-object"),
		Error,
		"must be a JSON object",
	);
});

Deno.test("parseEventTimelineRecord reads the T, L and M indexes", async (t) => {
	const cases = [
		{ input: { T: 42, data: "payload" }, expected: { wallclock: 42, data: "payload" } },
		{
			input: { L: [3, 7], data: null },
			expected: { location: { groupId: 3, objectId: 7 }, data: null },
		},
		{ input: { M: 100, data: [1, 2, 3] }, expected: { mediaTime: 100, data: [1, 2, 3] } },
	];
	for (const c of cases) {
		await t.step(Object.keys(c.input)[0] ?? "", () => {
			assertEquals(parseEventTimelineRecord(c.input), c.expected);
		});
	}
});

Deno.test("parseEventTimelineRecord does not read the draft-01 lowercase index keys", () => {
	assertThrows(
		() => parseEventTimelineRecord({ t: 1, data: "x" }),
		Error,
		"exactly one of T, L, or M",
	);
});

Deno.test("parseEventTimelineRecord preserves extraFields", () => {
	const record = parseEventTimelineRecord({ T: 1, data: "x", custom: "hello", num: 99 });

	assertEquals(record.extraFields, { custom: "hello", num: 99 });
});

Deno.test("encodeEventTimelineRecord writes the T, L and M keys", () => {
	const encoded = encodeEventTimelineRecord({
		location: { groupId: 3, objectId: 7 },
		data: { score: 1 },
		extraFields: { custom: true },
	});

	assertEquals(encoded, { custom: true, L: [3, 7], data: { score: 1 } });
});
