import { assertEquals, assertInstanceOf, assertRejects, assertStringIncludes } from "@std/assert";
import {
	type Catalog,
	catalogRefs,
	CatalogWalkError,
	type CatalogWalkErrorKind,
	type CatalogWalkLimits,
	type FullTrackName,
	parseCatalog,
	type Track,
	walkCatalogRefs,
} from "./mod.ts";

function refTrack(name: string, namespace?: string): Track {
	return { name, packaging: "catalog", isLive: true, ...(namespace ? { namespace } : {}) };
}

const ROOT_NS = "example.com/live";

function id(name: string, namespace = ROOT_NS): FullTrackName {
	return { namespace, name };
}

function key(id: FullTrackName): string {
	return `${id.namespace}/${id.name}`;
}

/**
 * An in-memory catalog source: catalogs maps `namespace/name` to the tracks
 * of the catalog published there. It records each load and visit.
 */
class FakeCatalogSource {
	catalogs = new Map<string, Track[]>();
	loadError: Error | undefined;
	visitError: Error | undefined;
	loads: FullTrackName[] = [];
	visits: { id: FullTrackName; defaultNamespace?: string }[] = [];

	load = (target: FullTrackName): Promise<Catalog> => {
		this.loads.push(target);
		const tracks = this.catalogs.get(key(target));
		if (this.loadError || tracks === undefined) {
			return Promise.reject(this.loadError ?? new Error(`no catalog ${key(target)}`));
		}
		return Promise.resolve({ version: "draft-02", tracks });
	};

	visit = (target: FullTrackName, catalog: Catalog): void => {
		if (this.visitError) {
			throw this.visitError;
		}
		this.visits.push({ id: target, defaultNamespace: catalog.defaultNamespace });
	};
}

// wide gives the root fanOut references to catalogs that each reference
// fanOut more, to model exponential fan-out within the depth limit.
function wide(fanOut: number): Map<string, Track[]> {
	const catalogs = new Map<string, Track[]>();
	const refs: Track[] = [];
	for (let i = 0; i < fanOut; i++) {
		refs.push(refTrack(`n${i}`));
		const children: Track[] = [];
		for (let j = 0; j < fanOut; j++) {
			children.push(refTrack(`n${i}-${j}`));
			catalogs.set(key(id(`n${i}-${j}`)), []);
		}
		catalogs.set(key(id(`n${i}`)), children);
	}
	catalogs.set(key(id("catalog")), refs);
	return catalogs;
}

Deno.test("catalogRefs lists the catalog tracks of the §5.6.16 example", () => {
	const catalog = parseCatalog(`{
		"version": "draft-02",
		"tracks": [
			{"name": "premium-catalog", "isLive": true, "packaging": "catalog"},
			{"name": "video", "isLive": true, "packaging": "loc"},
			{"name": "camera33-catalog", "namespace": "example.com/blimps-R-us", "packaging": "catalog", "isLive": true}
		]
	}`);
	catalog.defaultNamespace = ROOT_NS;

	assertEquals(catalogRefs(catalog), [
		id("premium-catalog"),
		id("camera33-catalog", "example.com/blimps-R-us"),
	]);
});

Deno.test("walkCatalogRefs visits each catalog once, depth first", async (t) => {
	const cases: {
		name: string;
		catalogs: Map<string, Track[]>;
		visited: string[];
	}[] = [
		{
			name: "tree with inherited and declared namespaces",
			catalogs: new Map([
				[key(id("catalog")), [refTrack("a"), refTrack("c", "other.com/feeds")]],
				[key(id("a")), []],
				[key(id("c", "other.com/feeds")), []],
			]),
			visited: [key(id("catalog")), key(id("a")), "other.com/feeds/c"],
		},
		{
			name: "diamond",
			catalogs: new Map([
				[key(id("catalog")), [refTrack("a"), refTrack("b")]],
				[key(id("a")), [refTrack("b")]],
				[key(id("b")), []],
			]),
			visited: [key(id("catalog")), key(id("a")), key(id("b"))],
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const source = new FakeCatalogSource();
			source.catalogs = c.catalogs;

			await walkCatalogRefs(id("catalog"), source.load, source.visit);

			assertEquals(source.visits.map((v) => key(v.id)), c.visited);
			assertEquals(source.loads.length, c.visited.length);
		});
	}
});

Deno.test("walkCatalogRefs gives a loaded catalog the namespace it was loaded from", async () => {
	const source = new FakeCatalogSource();
	source.catalogs = new Map([
		[key(id("catalog")), [refTrack("c", "other.com/feeds")]],
		[key(id("c", "other.com/feeds")), []],
	]);

	await walkCatalogRefs(id("catalog"), source.load, source.visit);

	assertEquals(source.visits.map((v) => v.defaultNamespace), [ROOT_NS, "other.com/feeds"]);
});

Deno.test("walkCatalogRefs walks a wide fan-out within the limits", async () => {
	const source = new FakeCatalogSource();
	source.catalogs = wide(3);

	await walkCatalogRefs(id("catalog"), source.load, source.visit);

	assertEquals(source.visits.length, 13);
});

Deno.test("walkCatalogRefs stops on a cycle or an exceeded limit", async (t) => {
	const cases: {
		name: string;
		catalogs: Map<string, Track[]>;
		limits?: CatalogWalkLimits;
		kind: CatalogWalkErrorKind;
		visits: number;
	}[] = [
		{
			name: "self reference",
			catalogs: new Map([[key(id("catalog")), [refTrack("catalog")]]]),
			kind: "cycle",
			visits: 1,
		},
		{
			name: "two catalog cycle",
			catalogs: new Map([
				[key(id("catalog")), [refTrack("a")]],
				[key(id("a")), [refTrack("catalog")]],
			]),
			kind: "cycle",
			visits: 2,
		},
		{
			name: "depth",
			catalogs: new Map([
				[key(id("catalog")), [refTrack("a")]],
				[key(id("a")), [refTrack("b")]],
				[key(id("b")), []],
			]),
			limits: { maxDepth: 1 },
			kind: "depth",
			visits: 2,
		},
		{
			name: "catalog count",
			catalogs: wide(10),
			limits: { maxCatalogs: 20 },
			kind: "tooManyCatalogs",
			visits: 20,
		},
		{
			name: "references in one catalog",
			catalogs: wide(5),
			limits: { maxRefsPerCatalog: 4 },
			kind: "tooManyRefs",
			visits: 0,
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const source = new FakeCatalogSource();
			source.catalogs = c.catalogs;

			const err = await assertRejects(() =>
				walkCatalogRefs(id("catalog"), source.load, source.visit, { limits: c.limits })
			);

			assertInstanceOf(err, CatalogWalkError);
			assertEquals([err.kind, source.visits.length], [c.kind, c.visits]);
		});
	}
});

Deno.test("walkCatalogRefs names the path of a cycle", async () => {
	const source = new FakeCatalogSource();
	source.catalogs = new Map([
		[key(id("catalog")), [refTrack("a")]],
		[key(id("a")), [refTrack("catalog")]],
	]);

	const err = await assertRejects(() =>
		walkCatalogRefs(id("catalog"), source.load, source.visit)
	);

	assertInstanceOf(err, Error);
	assertStringIncludes(
		err.message,
		"example.com/live/catalog -> example.com/live/a -> example.com/live/catalog",
	);
});

Deno.test("walkCatalogRefs wraps load and visit errors as the cause", async (t) => {
	for (const which of ["load", "visit"] as const) {
		await t.step(which, async () => {
			const source = new FakeCatalogSource();
			source.catalogs = new Map([[key(id("catalog")), []]]);
			const cause = new Error(`${which} failed`);
			if (which === "load") {
				source.loadError = cause;
			} else {
				source.visitError = cause;
			}

			const err = await assertRejects(() =>
				walkCatalogRefs(id("catalog"), source.load, source.visit)
			);

			assertInstanceOf(err, Error);
			assertEquals(err.cause, cause);
		});
	}
});

Deno.test("walkCatalogRefs stops before loading once its signal is aborted", async () => {
	const source = new FakeCatalogSource();
	source.catalogs = new Map([[key(id("catalog")), []]]);
	const controller = new AbortController();
	controller.abort();

	await assertRejects(() =>
		walkCatalogRefs(id("catalog"), source.load, source.visit, { signal: controller.signal })
	);

	assertEquals(source.loads, []);
});

Deno.test("walkCatalogRefs rejects a root it cannot load", async (t) => {
	const cases: { name: string; root: FullTrackName; message: string }[] = [
		{
			name: "no namespace",
			root: { namespace: "", name: "catalog" },
			message: "needs the Track Namespace",
		},
		{
			name: "inherited marker",
			root: { namespace: "\u0000catalog", name: "catalog" },
			message: "needs the Track Namespace",
		},
		{
			name: "malformed namespace",
			root: { namespace: "a//b", name: "catalog" },
			message: "needs the Track Namespace",
		},
		{ name: "no name", root: { namespace: "live", name: "" }, message: "Track Name" },
		{
			name: "NUL in name",
			root: { namespace: "live", name: "cat\u0000alog" },
			message: "Track Name",
		},
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const source = new FakeCatalogSource();

			await assertRejects(
				() => walkCatalogRefs(c.root, source.load, source.visit),
				Error,
				c.message,
			);
			assertEquals(source.loads, []);
		});
	}
});

Deno.test("walkCatalogRefs checks a reference before loading it", async (t) => {
	const cases: { name: string; ref: Track }[] = [
		{ name: "no name", ref: refTrack("") },
		{ name: "malformed namespace", ref: refTrack("child", "a//b") },
		{ name: "NUL in name", ref: refTrack("ch\u0000ild") },
	];
	for (const c of cases) {
		await t.step(c.name, async () => {
			const source = new FakeCatalogSource();
			source.catalogs = new Map([[key(id("catalog")), [c.ref]]]);

			await assertRejects(
				() => walkCatalogRefs(id("catalog"), source.load, source.visit),
				Error,
				"references an invalid catalog track",
			);
			assertEquals(source.loads, [id("catalog")]);
		});
	}
});
