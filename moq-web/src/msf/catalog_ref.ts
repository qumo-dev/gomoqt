/**
 * Following catalog references: tracks with packaging `"catalog"` that point
 * at other MSF catalogs (draft-ietf-moq-msf-02 §5, §13.7).
 *
 * @module
 */
import {
	type Catalog,
	displayName,
	effectiveNamespace,
	identityKey,
	INHERITED_NAMESPACE,
} from "./catalog.ts";
import { namespaceProblem, nameTextProblem } from "./names.ts";

/** Default longest chain of catalog references followed from the root. */
export const DefaultMaxCatalogDepth = 8;
/** Default number of distinct catalogs loaded in one walk, the root included. */
export const DefaultMaxCatalogs = 64;
/** Default number of catalog references a single catalog may hold. */
export const DefaultMaxCatalogRefs = 32;

/** A Full Track Name: a Track Namespace and a Track Name. */
export interface FullTrackName {
	namespace: string;
	name: string;
}

/**
 * Bounds on what {@link walkCatalogRefs} spends, since subscribers should
 * bound the references they follow (draft-ietf-moq-msf-02 §13.7). A field
 * that is absent, zero or negative takes its default.
 */
export interface CatalogWalkLimits {
	/** Longest chain of references followed from the root ({@link DefaultMaxCatalogDepth}). */
	maxDepth?: number;
	/**
	 * Distinct catalogs loaded, the root included ({@link DefaultMaxCatalogs}).
	 * It bounds wide fan-out, which a depth limit alone lets grow exponentially.
	 */
	maxCatalogs?: number;
	/** Catalog references one catalog may hold ({@link DefaultMaxCatalogRefs}). */
	maxRefsPerCatalog?: number;
}

/** Why {@link walkCatalogRefs} stopped on a {@link CatalogWalkError}. */
export type CatalogWalkErrorKind = "cycle" | "depth" | "tooManyCatalogs" | "tooManyRefs";

/** Error thrown when a catalog walk finds a cycle or exceeds a limit. */
export class CatalogWalkError extends Error {
	readonly kind: CatalogWalkErrorKind;

	constructor(kind: CatalogWalkErrorKind, message: string) {
		super(message);
		this.name = "CatalogWalkError";
		this.kind = kind;
	}
}

/** Options for {@link walkCatalogRefs}. */
export interface WalkCatalogRefsOptions {
	limits?: CatalogWalkLimits;
	/** Stops the walk before the next load once aborted. */
	signal?: AbortSignal;
}

/**
 * Return the Full Track Names of the tracks in catalog that reference other
 * catalogs (packaging `"catalog"`), in catalog order. A track without a
 * namespace inherits the catalog's default namespace.
 */
export function catalogRefs(catalog: Catalog): FullTrackName[] {
	return catalog.tracks
		.filter((track) => track.packaging === "catalog")
		.map((track) => ({
			namespace: effectiveNamespace(track, catalog.defaultNamespace),
			name: track.name ?? "",
		}));
}

function catalogTrackProblem(id: FullTrackName): string | undefined {
	const namespace = namespaceProblem(id.namespace);
	if (namespace !== undefined) {
		return `catalog ${
			JSON.stringify(id.name)
		} needs the Track Namespace it is published under: ${namespace}`;
	}
	if (id.name === "") {
		return "catalog needs a non-empty Track Name";
	}
	const name = nameTextProblem(id.name);
	return name === undefined ? undefined : `catalog Track Name: ${name}`;
}

function positive(value: number | undefined, fallback: number): number {
	return value !== undefined && value > 0 ? value : fallback;
}

/**
 * Visit the root catalog and, depth first, every catalog it references
 * through packaging `"catalog"` tracks. load fetches the catalog published on
 * a track; a loaded catalog that sets no default namespace gets the
 * namespace of the track it was loaded from, so its tracks inherit it. visit
 * is called once per distinct catalog, before its references are followed.
 *
 * Each distinct catalog loads at most once: one reached again by another
 * path is skipped. A reference back to a catalog on the current path is a
 * cycle (§13.7). Each reference's namespace and name are checked before it
 * is loaded, as the referencing catalog is not validated.
 * @throws {@link CatalogWalkError} on a cycle or an exceeded limit.
 * @throws Error if the root or a reference does not name a loadable catalog
 * track, or wrapping (as `cause`) an error from load or visit.
 */
export async function walkCatalogRefs(
	root: FullTrackName,
	load: (id: FullTrackName, signal: AbortSignal | undefined) => Promise<Catalog>,
	visit: (id: FullTrackName, catalog: Catalog) => void | Promise<void>,
	options: WalkCatalogRefsOptions = {},
): Promise<void> {
	// The root's namespace is what its tracks inherit, so the walk needs it;
	// the internal inherited-namespace marker never reaches load.
	const start = root.namespace === INHERITED_NAMESPACE ? { ...root, namespace: "" } : root;
	const rootProblem = catalogTrackProblem(start);
	if (rootProblem !== undefined) {
		throw new Error(`msf: root catalog: ${rootProblem}`);
	}
	const limits = {
		maxDepth: positive(options.limits?.maxDepth, DefaultMaxCatalogDepth),
		maxCatalogs: positive(options.limits?.maxCatalogs, DefaultMaxCatalogs),
		maxRefsPerCatalog: positive(options.limits?.maxRefsPerCatalog, DefaultMaxCatalogRefs),
	};
	const visited = new Set<string>();
	const path: FullTrackName[] = [];
	const describe = (next: FullTrackName): string =>
		[...path, next].map((id) => displayName(id.namespace, id.name)).join(" -> ");

	const walk = async (id: FullTrackName, depth: number): Promise<void> => {
		const key = identityKey(id.namespace, id.name);
		if (path.some((onPath) => identityKey(onPath.namespace, onPath.name) === key)) {
			throw new CatalogWalkError("cycle", `msf: catalog reference cycle: ${describe(id)}`);
		}
		if (visited.has(key)) {
			return;
		}
		if (depth > limits.maxDepth) {
			throw new CatalogWalkError(
				"depth",
				`msf: catalog reference depth exceeded: ${describe(id)}`,
			);
		}
		if (visited.size >= limits.maxCatalogs) {
			throw new CatalogWalkError(
				"tooManyCatalogs",
				`msf: too many referenced catalogs: limit ${limits.maxCatalogs} reached at ${
					describe(id)
				}`,
			);
		}
		options.signal?.throwIfAborted();

		const shown = JSON.stringify(displayName(id.namespace, id.name));
		let loaded: Catalog;
		try {
			loaded = await load(id, options.signal);
		} catch (e) {
			throw new Error(`msf: load catalog ${shown}`, { cause: e });
		}
		const catalog = loaded.defaultNamespace
			? loaded
			: { ...loaded, defaultNamespace: id.namespace };
		visited.add(key);
		const refs = catalogRefs(catalog);
		if (refs.length > limits.maxRefsPerCatalog) {
			throw new CatalogWalkError(
				"tooManyRefs",
				`msf: too many catalog references in one catalog: catalog ${shown} holds ${refs.length}, limit ${limits.maxRefsPerCatalog}`,
			);
		}
		try {
			await visit(id, catalog);
		} catch (e) {
			throw new Error(`msf: visit catalog ${shown}`, { cause: e });
		}

		path.push(id);
		try {
			for (const ref of refs) {
				const problem = catalogTrackProblem(ref);
				if (problem !== undefined) {
					throw new Error(
						`msf: catalog ${shown} references an invalid catalog track: ${problem}`,
					);
				}
				await walk(ref, depth + 1);
			}
		} finally {
			path.pop();
		}
	};
	await walk(start, 0);
}
