import { assertEquals, assertThrows } from "@std/assert";
import {
	type Catalog,
	parseFragmentVariables,
	resolveCatalogVariables,
	resolveVariables,
	validateVariableSyntax,
	ValidationError,
} from "./mod.ts";

Deno.test("parseFragmentVariables splits pairs at their first =", () => {
	const vars = parseFragmentVariables("#msf:ns--catalog&token=a=b&id=1&id=2&=x&empty=");

	assertEquals([...vars], [["token", "a=b"], ["id", "1"], ["empty", ""]]);
});

Deno.test("resolveVariables substitutes references or fails", async (t) => {
	const vars = new Map([["token", "ab+/=.@-_9"], ["bad", "a b"], ["id", "7"]]);
	const cases: { input: string; expected?: string }[] = [
		{ input: "plain", expected: "plain" },
		{ input: "x-%token%-%id%", expected: "x-ab+/=.@-_9-7" },
		{ input: "%missing%" },
		{ input: "%bad%" },
		{ input: "50%" },
		{ input: "%%" },
	];
	for (const c of cases) {
		await t.step(JSON.stringify(c.input), () => {
			assertEquals(resolveVariables(c.input, vars), c.expected);
		});
	}
});

Deno.test("validateVariableSyntax rejects malformed references", async (t) => {
	const cases = [
		{ input: "a%b", message: "unterminated variable reference" },
		{ input: "a%%b", message: "empty variable name" },
	];
	for (const c of cases) {
		await t.step(c.input, () => {
			assertThrows(() => validateVariableSyntax(c.input), Error, c.message);
		});
	}
});

Deno.test("validateVariableSyntax accepts well-formed references", () => {
	validateVariableSyntax("a-%x%-%any name%");
});

const template: Catalog = {
	version: "draft-02",
	defaultNamespace: "live",
	tracks: [{
		name: "tracking-%id%",
		packaging: "eventtimeline",
		eventType: "e",
		isLive: true,
		authInfo: { cat: "%token%", nested: ["%token%"] },
		extraFields: { "x-note": "%token%" },
	}],
};

Deno.test("resolveCatalogVariables substitutes every string value, then validates", () => {
	const resolved = resolveCatalogVariables(template, new Map([["id", "c3"], ["token", "t1"]]));

	assertEquals(resolved.tracks[0], {
		name: "tracking-c3",
		packaging: "eventtimeline",
		eventType: "e",
		isLive: true,
		authInfo: { cat: "t1", nested: ["t1"] },
		extraFields: { "x-note": "t1" },
	});
	assertEquals(resolved.defaultNamespace, "live");
});

Deno.test("resolveCatalogVariables leaves a connectionURI variable unsubstituted, as an error", () => {
	const catalog: Catalog = {
		version: "draft-02",
		tracks: [],
		publishTracks: [{
			name: "3",
			packaging: "moqlog",
			role: "log",
			isLive: true,
			connectionURI: "moqt://%host%/logs",
		}],
	};

	assertThrows(
		() => resolveCatalogVariables(catalog, new Map([["host", "relay"]])),
		Error,
		"variables in connectionURI are not substituted",
	);
});

Deno.test("resolveCatalogVariables rejects an unresolvable reference", () => {
	assertThrows(
		() => resolveCatalogVariables(template, new Map([["id", "c3"]])),
		Error,
		'failed to resolve variables in "%token%"',
	);
});

Deno.test("resolveCatalogVariables validates the substituted names", () => {
	const catalog: Catalog = {
		version: "draft-02",
		tracks: [{ name: "a", packaging: "cmaf", isLive: true, namespace: "%ns%" }],
	};

	assertThrows(
		() => resolveCatalogVariables(catalog, new Map([["ns", "x//y"]])),
		ValidationError,
		"contains an empty field",
	);
});
