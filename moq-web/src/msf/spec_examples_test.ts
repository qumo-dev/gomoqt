import { assertEquals } from "@std/assert";
import {
	parseCatalog,
	parseCatalogDelta,
	parseFragmentVariables,
	resolveCatalogVariables,
	stringifyCatalog,
	stringifyCatalogDelta,
	validateCatalog,
	validateCatalogDelta,
} from "./mod.ts";

// The catalog examples of draft-ietf-moq-msf-02 §5.6, extracted verbatim from
// the draft into the Go package's testdata, so both implementations check
// the same documents.
const EXAMPLE_DIR = new URL("../../../msf/testdata/draft02/", import.meta.url);

async function readExamples(): Promise<Map<string, string>> {
	const examples = new Map<string, string>();
	for await (const entry of Deno.readDir(EXAMPLE_DIR)) {
		if (entry.isFile && entry.name.endsWith(".json")) {
			examples.set(entry.name, await Deno.readTextFile(new URL(entry.name, EXAMPLE_DIR)));
		}
	}
	return new Map([...examples].sort(([a], [b]) => a.localeCompare(b)));
}

const examples = await readExamples();

Deno.test("the draft has 18 catalog example documents", () => {
	assertEquals(examples.size, 18);
});

Deno.test("every draft-02 example parses, validates and round-trips", async (t) => {
	for (const [name, json] of examples) {
		await t.step(name, () => {
			if (json.includes('"deltaUpdate"')) {
				const delta = parseCatalogDelta(json);
				validateCatalogDelta(delta);
				const again = parseCatalogDelta(stringifyCatalogDelta(delta));
				assertEquals(stringifyCatalogDelta(again), stringifyCatalogDelta(delta));
				return;
			}
			const catalog = parseCatalog(json);
			validateCatalog(catalog);
			const again = parseCatalog(stringifyCatalog(catalog));
			validateCatalog(again);
			assertEquals(stringifyCatalog(again), stringifyCatalog(catalog));
		});
	}
});

Deno.test("the §5.6.14 template resolves to the catalog the draft gives", () => {
	const template = parseCatalog(examples.get("5.6.14-1.json") ?? "");
	const want = parseCatalog(examples.get("5.6.14-2.json") ?? "");
	const vars = parseFragmentVariables("msf:namespace--catalog&token=1234&id=c3e5s7w&event=xyz");

	const got = resolveCatalogVariables(template, vars);

	assertEquals(JSON.parse(stringifyCatalog(got)), JSON.parse(stringifyCatalog(want)));
});
