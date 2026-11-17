import { z } from "zod";
import {
	applyTrackOverrides,
	type Catalog,
	cloneCatalog,
	cloneTrack,
	displayName,
	displayTrack,
	effectiveNamespace,
	EMPTY_NAMESPACE_MESSAGE,
	parseTrack,
	type Track,
	trackKey,
	trackProblems,
	trackToJSON,
	validateCatalog,
	ValidationError,
} from "./catalog.ts";
import { asRecord, decodeCatalogJSON, splitKnown, zodSchemaError } from "./json.ts";
import { namespaceProblem, nameTextProblem } from "./names.ts";

/** Discriminator for the four delta operations. */
export type DeltaOperationKind = "addTracks" | "removeTracks" | "cloneTracks" | "updateTracks";

/**
 * One operation of a delta's `deltaUpdate` array, in wire order: its kind and
 * how many tracks it takes, in order, from the delta's array for that kind.
 */
export interface DeltaOp {
	kind: DeltaOperationKind;
	count: number;
}

/** Identifies a track for removal in a delta update. */
export interface TrackRef {
	namespace?: string;
	name?: string;
	extraFields?: Record<string, unknown>;
}

/**
 * A "clone" operation: a new track derived from a parent track. The fields
 * {@link track} declares override the parent's; it must declare a new name.
 */
export interface TrackClone {
	track: Track;
	parentName?: string;
	/** Namespace of the parent track; the catalog namespace when absent. */
	parentNamespace?: string;
}

/**
 * An "update" operation (draft-ietf-moq-msf-02 §5.1.6): the fields
 * {@link track} declares override those of the existing track that
 * {@link parentName} and {@link parentNamespace} identify. Declaring name or
 * namespace renames the track.
 */
export interface TrackUpdate {
	track: Track;
	parentName?: string;
	/** Namespace of the track to update; the catalog namespace when absent. */
	parentNamespace?: string;
}

/**
 * An MSF delta update that adds, removes, clones or updates tracks of an
 * existing {@link Catalog} (draft-ietf-moq-msf-02 §5.1.6, §5.3).
 *
 * Its tracks are grouped by operation kind. {@link deltaOps} records the
 * wire operations in order, so that interleaved operations (remove, add,
 * remove of the same track) apply and re-encode in their declared order;
 * tracks beyond what it covers follow as one operation per kind, in add,
 * remove, clone, update order, so an update can target a track the same
 * delta adds or clones.
 */
export interface CatalogDelta {
	/**
	 * When set, replaces the base catalog's namespace for tracks that declare
	 * none. Not serialized.
	 */
	defaultNamespace?: string;
	/** Required in a delta. */
	generatedAt?: number;
	addTracks: Track[];
	removeTracks: TrackRef[];
	cloneTracks: TrackClone[];
	updateTracks: TrackUpdate[];
	extraFields?: Record<string, unknown>;
	deltaOps?: DeltaOp[];
}

/** Error thrown when a delta is applied to a catalog marked isComplete. */
export class CatalogCompleteError extends Error {
	constructor() {
		super("msf: cannot apply a delta to a complete catalog");
		this.name = "CatalogCompleteError";
	}
}

/** The order code-built operations apply and encode in. */
const KINDS: readonly DeltaOperationKind[] = [
	"addTracks",
	"removeTracks",
	"cloneTracks",
	"updateTracks",
];
// In-memory operation kind -> wire op name, and back.
const KIND_TO_OP: Readonly<Record<DeltaOperationKind, string>> = {
	addTracks: "add",
	removeTracks: "remove",
	cloneTracks: "clone",
	updateTracks: "update",
};
const OP_TO_KIND: ReadonlyMap<string, DeltaOperationKind> = new Map(
	KINDS.map((kind) => [KIND_TO_OP[kind], kind]),
);

/** Root catalog fields a delta must not carry (§5.3). */
const FORBIDDEN_DELTA_KEYS = ["version", "isComplete", "tracks", "publishTracks", "initDataList"];

const deltaRootSchema = z.object({
	deltaUpdate: z.array(z.unknown()).nullable(),
	generatedAt: z.number().int().nullish(),
});

const trackRefSchema = z.object({
	namespace: z.string().nullish(),
	name: z.string().nullish(),
});

const parentSchema = z.object({
	parentName: z.string().nullish(),
	parentNamespace: z.string().nullish(),
});

const TRACK_REF_KEYS: ReadonlySet<string> = new Set(["namespace", "name"]);

function parseTrackRef(value: unknown): TrackRef {
	const where = "msf: remove track reference must be a JSON object";
	const [knownRaw, extraFields] = splitKnown(asRecord(value, where), TRACK_REF_KEYS);
	const parsed = trackRefSchema.safeParse(knownRaw);
	if (!parsed.success) {
		throw zodSchemaError(where, parsed.error);
	}
	const { namespace, name } = parsed.data;
	if (namespace === "") {
		throw new Error(EMPTY_NAMESPACE_MESSAGE);
	}
	const ref: TrackRef = {};
	if (namespace) {
		ref.namespace = namespace;
	}
	if (name) {
		ref.name = name;
	}
	if (extraFields) {
		ref.extraFields = extraFields;
	}
	return ref;
}

/** Decodes a clone or update entry: the track overrides plus the parent fields. */
function parseParentEntry(value: unknown, op: string): TrackClone {
	const where = `msf: ${op} track entry must be a JSON object`;
	const { parentName, parentNamespace, ...trackRecord } = asRecord(value, where);
	const parsed = parentSchema.safeParse({ parentName, parentNamespace });
	if (!parsed.success) {
		throw zodSchemaError(where, parsed.error);
	}
	if (parsed.data.parentNamespace === "") {
		throw new Error(
			"msf: parentNamespace must not be empty; omit it to use the catalog namespace",
		);
	}
	const entry: TrackClone = { track: parseTrack(trackRecord) };
	if (parsed.data.parentName) {
		entry.parentName = parsed.data.parentName;
	}
	if (parsed.data.parentNamespace) {
		entry.parentNamespace = parsed.data.parentNamespace;
	}
	return entry;
}

function parseOperation(value: unknown): { op: string; tracks: unknown[] } {
	const operation = asRecord(value, "msf: deltaUpdate must be an array of operation objects");
	if (!("op" in operation)) {
		throw new Error("msf: delta update operation must contain an op field");
	}
	if (!("tracks" in operation)) {
		throw new Error("msf: delta update operation must contain a tracks field");
	}
	const parsed = z.object({
		op: z.string().nullable(),
		tracks: z.array(z.unknown()).nullable(),
	}).safeParse(operation);
	if (!parsed.success) {
		throw zodSchemaError("msf: delta update operation", parsed.error);
	}
	return { op: parsed.data.op ?? "", tracks: parsed.data.tracks ?? [] };
}

/**
 * Parse a JSON delta-catalog payload into a {@link CatalogDelta}. A
 * missing generatedAt is reported by {@link validateCatalogDelta}.
 * @param data - UTF-8 encoded bytes or a JSON string.
 * @throws Error if the payload has no deltaUpdate array, carries a root
 * catalog field a delta must not (version, isComplete, tracks,
 * publishTracks, initDataList), holds an unknown op, or breaks the text
 * rules {@link parseCatalog} enforces.
 */
export function parseCatalogDelta(data: string | Uint8Array): CatalogDelta {
	const root = asRecord(decodeCatalogJSON(data), "msf: expected JSON object");
	for (const key of FORBIDDEN_DELTA_KEYS) {
		if (key in root) {
			throw new Error(
				`msf: root catalog field ${JSON.stringify(key)} is not allowed in a delta catalog`,
			);
		}
	}
	if (!("deltaUpdate" in root)) {
		throw new Error("msf: delta catalog must include a deltaUpdate array");
	}
	const [knownRaw, extraFields] = splitKnown(root, new Set(["deltaUpdate", "generatedAt"]));
	const parsed = deltaRootSchema.safeParse(knownRaw);
	if (!parsed.success) {
		throw zodSchemaError("msf: expected JSON object", parsed.error);
	}

	const delta: CatalogDelta = {
		addTracks: [],
		removeTracks: [],
		cloneTracks: [],
		updateTracks: [],
		deltaOps: [],
	};
	for (const value of parsed.data.deltaUpdate ?? []) {
		const { op, tracks } = parseOperation(value);
		const kind = OP_TO_KIND.get(op);
		switch (kind) {
			case "addTracks":
				delta.addTracks.push(...tracks.map(parseTrack));
				break;
			case "removeTracks":
				delta.removeTracks.push(...tracks.map(parseTrackRef));
				break;
			case "cloneTracks":
				delta.cloneTracks.push(...tracks.map((track) => parseParentEntry(track, "clone")));
				break;
			case "updateTracks":
				delta.updateTracks.push(
					...tracks.map((track) => parseParentEntry(track, "update")),
				);
				break;
			case undefined:
				throw new Error(`msf: unknown delta update op ${JSON.stringify(op)}`);
			default: {
				const _: never = kind;
				return _;
			}
		}
		delta.deltaOps?.push({ kind, count: tracks.length });
	}
	if (parsed.data.generatedAt !== undefined && parsed.data.generatedAt !== null) {
		delta.generatedAt = parsed.data.generatedAt;
	}
	if (extraFields) {
		delta.extraFields = extraFields;
	}
	return delta;
}

/** An operation to apply or encode: a kind and the slice of its array it covers. */
interface DeltaSpan {
	kind: DeltaOperationKind;
	lo: number;
	hi: number;
}

/**
 * Returns the delta's operations in the order to apply and encode them:
 * recorded operations in order, then the tracks beyond what they cover as
 * one operation per kind.
 */
function operations(delta: CatalogDelta): DeltaSpan[] {
	const lengths: Record<DeltaOperationKind, number> = {
		addTracks: delta.addTracks.length,
		removeTracks: delta.removeTracks.length,
		cloneTracks: delta.cloneTracks.length,
		updateTracks: delta.updateTracks.length,
	};
	const next: Record<DeltaOperationKind, number> = {
		addTracks: 0,
		removeTracks: 0,
		cloneTracks: 0,
		updateTracks: 0,
	};
	const spans: DeltaSpan[] = [];
	for (const op of delta.deltaOps ?? []) {
		const lo = next[op.kind];
		const hi = Math.min(lo + op.count, lengths[op.kind]);
		// An empty operation is kept so the wire array round-trips.
		if (hi > lo || op.count === 0) {
			spans.push({ kind: op.kind, lo, hi });
		}
		next[op.kind] = hi;
	}
	for (const kind of KINDS) {
		if (next[kind] < lengths[kind]) {
			spans.push({ kind, lo: next[kind], hi: lengths[kind] });
		}
	}
	return spans;
}

function parentProblems(
	op: string,
	parentName: string | undefined,
	parentNamespace: string | undefined,
): string[] {
	const problems: string[] = [];
	if (!parentName) {
		problems.push(`parentName is required for ${op} tracks`);
	} else {
		const problem = nameTextProblem(parentName);
		if (problem !== undefined) {
			problems.push(`parentName: ${problem}`);
		}
	}
	if (parentNamespace) {
		const problem = namespaceProblem(parentNamespace);
		if (problem !== undefined) {
			problems.push(`parentNamespace: ${problem}`);
		}
	}
	return problems;
}

function trackRefProblems(ref: TrackRef): string[] {
	const problems: string[] = [];
	if (!ref.name) {
		problems.push("name is required");
	} else {
		const problem = nameTextProblem(ref.name);
		if (problem !== undefined) {
			problems.push(`name: ${problem}`);
		}
	}
	if (ref.namespace) {
		const problem = namespaceProblem(ref.namespace);
		if (problem !== undefined) {
			problems.push(problem);
		}
	}
	if (ref.extraFields && Object.keys(ref.extraFields).length > 0) {
		problems.push("remove track entries may contain only name and optional namespace");
	}
	return problems;
}

/**
 * Validate a {@link CatalogDelta} according to the MSF draft-02 rules:
 * generatedAt is present, some operation carries a track, and each track is
 * well formed. Whether the operations fit a catalog is checked by
 * {@link applyCatalogDelta}.
 * @throws {@link ValidationError} if any problems are found.
 */
export function validateCatalogDelta(delta: CatalogDelta): void {
	const problems: string[] = [];
	if (delta.generatedAt === undefined || delta.generatedAt === null) {
		problems.push("delta catalog must include generatedAt");
	}
	if (
		delta.addTracks.length === 0 && delta.removeTracks.length === 0 &&
		delta.cloneTracks.length === 0 && delta.updateTracks.length === 0
	) {
		problems.push(
			"delta catalog must contain at least one add, remove, clone, or update track",
		);
	}
	const push = (prefix: string, list: string[]) => {
		for (const problem of list) {
			problems.push(`${prefix}: ${problem}`);
		}
	};
	delta.addTracks.forEach((track, i) => push(`add[${i}]`, trackProblems(track, "tracks")));
	delta.removeTracks.forEach((ref, i) => push(`remove[${i}]`, trackRefProblems(ref)));
	delta.cloneTracks.forEach((clone, i) =>
		push(`clone[${i}]`, [
			...(clone.track.name ? [] : ["name is required"]),
			...parentProblems("clone", clone.parentName, clone.parentNamespace),
		])
	);
	delta.updateTracks.forEach((update, i) =>
		push(`update[${i}]`, parentProblems("update", update.parentName, update.parentNamespace))
	);
	if (problems.length > 0) {
		throw new ValidationError(problems);
	}
}

function findTrack(catalog: Catalog, key: string): number {
	return catalog.tracks.findIndex((track) => trackKey(track, catalog.defaultNamespace) === key);
}

function addTrack(catalog: Catalog, track: Track): void {
	const index = findTrack(catalog, trackKey(track, catalog.defaultNamespace));
	if (index >= 0) {
		throw new Error(
			`msf: cannot add duplicate track ${
				JSON.stringify(displayTrack(track, catalog.defaultNamespace))
			} at index ${index}`,
		);
	}
	catalog.tracks.push(cloneTrack(track));
}

function removeTrack(catalog: Catalog, ref: TrackRef): void {
	const index = findTrack(catalog, trackKey(ref, catalog.defaultNamespace));
	if (index < 0) {
		throw new Error(
			`msf: cannot remove unknown track ${
				JSON.stringify(displayTrack(ref, catalog.defaultNamespace))
			}`,
		);
	}
	catalog.tracks.splice(index, 1);
}

function parentIdentity(
	catalog: Catalog,
	entry: { parentName?: string; parentNamespace?: string },
): { key: string; display: string } {
	const namespace = effectiveNamespace(
		{ namespace: entry.parentNamespace },
		catalog.defaultNamespace,
	);
	const name = entry.parentName ?? "";
	return {
		key: trackKey({ namespace, name }, undefined),
		display: displayName(namespace, name),
	};
}

function cloneInto(catalog: Catalog, clone: TrackClone): void {
	const parent = parentIdentity(catalog, clone);
	const base = catalog.tracks[findTrack(catalog, parent.key)];
	if (base === undefined) {
		throw new Error(`msf: cannot clone unknown parent track ${JSON.stringify(parent.display)}`);
	}
	const derived = applyTrackOverrides(base, clone.track);
	const index = findTrack(catalog, trackKey(derived, catalog.defaultNamespace));
	if (index >= 0) {
		throw new Error(
			`msf: cannot clone into duplicate track ${
				JSON.stringify(displayTrack(derived, catalog.defaultNamespace))
			} at index ${index}`,
		);
	}
	catalog.tracks.push(derived);
}

// An update must not rename onto another track, and isLive must not go from
// false back to true (draft-ietf-moq-msf-02 §5.2.8).
function updateInto(catalog: Catalog, update: TrackUpdate): void {
	const target = parentIdentity(catalog, update);
	const index = findTrack(catalog, target.key);
	const current = catalog.tracks[index];
	if (current === undefined) {
		throw new Error(`msf: cannot update unknown track ${JSON.stringify(target.display)}`);
	}
	if (current.isLive === false && update.track.isLive === true) {
		throw new Error(
			`msf: cannot update track ${
				JSON.stringify(target.display)
			}: isLive must not change from false to true`,
		);
	}
	const updated = applyTrackOverrides(current, update.track);
	const updatedKey = trackKey(updated, catalog.defaultNamespace);
	if (updatedKey !== target.key) {
		const other = findTrack(catalog, updatedKey);
		if (other >= 0) {
			throw new Error(
				`msf: cannot update track ${JSON.stringify(target.display)} into duplicate track ${
					JSON.stringify(displayTrack(updated, catalog.defaultNamespace))
				} at index ${other}`,
			);
		}
	}
	catalog.tracks[index] = updated;
}

/**
 * Apply a delta to a base catalog and return the resulting catalog
 * (draft-ietf-moq-msf-02 §5.3). Operations apply in order, each to the
 * result of the previous one. An illogical operation (adding or cloning to
 * an existing Full Track Name, removing or updating an unknown track)
 * rejects the whole delta, as does a result that fails
 * {@link validateCatalog}.
 * @param baseCatalog - The current catalog state.
 * @param deltaCatalog - The delta to apply.
 * @throws {@link CatalogCompleteError} if the base catalog is marked
 * isComplete, which commits that nothing more changes (§5.1.3).
 * @throws {@link ValidationError} if the base, the delta or the result is
 * invalid.
 * @throws Error if an operation does not fit the catalog.
 */
export function applyCatalogDelta(baseCatalog: Catalog, deltaCatalog: CatalogDelta): Catalog {
	if (baseCatalog.isComplete) {
		throw new CatalogCompleteError();
	}
	validateCatalog(baseCatalog);
	validateCatalogDelta(deltaCatalog);

	const result = cloneCatalog(baseCatalog);
	if (
		deltaCatalog.defaultNamespace && deltaCatalog.defaultNamespace !== result.defaultNamespace
	) {
		const inherits = [...result.tracks, ...(result.publishTracks ?? [])]
			.some((track) => !track.namespace);
		if (inherits) {
			throw new Error(
				"msf: cannot change default namespace when catalog contains tracks that inherit it",
			);
		}
		result.defaultNamespace = deltaCatalog.defaultNamespace;
	}
	if (deltaCatalog.generatedAt !== undefined && deltaCatalog.generatedAt !== null) {
		result.generatedAt = deltaCatalog.generatedAt;
	}
	if (deltaCatalog.extraFields) {
		result.extraFields = {
			...result.extraFields,
			...structuredClone(deltaCatalog.extraFields),
		};
	}

	for (const { kind, lo, hi } of operations(deltaCatalog)) {
		switch (kind) {
			case "addTracks":
				deltaCatalog.addTracks.slice(lo, hi).forEach((track) => addTrack(result, track));
				break;
			case "removeTracks":
				deltaCatalog.removeTracks.slice(lo, hi).forEach((ref) => removeTrack(result, ref));
				break;
			case "cloneTracks":
				deltaCatalog.cloneTracks.slice(lo, hi).forEach((clone) => cloneInto(result, clone));
				break;
			case "updateTracks":
				deltaCatalog.updateTracks.slice(lo, hi).forEach((update) =>
					updateInto(result, update)
				);
				break;
		}
	}

	validateCatalog(result);
	return result;
}

function parentEntryToJSON(entry: TrackClone | TrackUpdate): Record<string, unknown> {
	const obj = trackToJSON(entry.track);
	if (entry.parentName) {
		obj.parentName = entry.parentName;
	}
	if (entry.parentNamespace) {
		obj.parentNamespace = entry.parentNamespace;
	}
	return obj;
}

/**
 * Serialize a {@link CatalogDelta} to its draft-02 JSON form: a
 * `deltaUpdate` array of `{op, tracks}` objects in declared operation order
 * (see {@link CatalogDelta}), plus generatedAt. An operation without tracks
 * is written with `tracks: []`. {@link CatalogDelta.defaultNamespace} is not
 * written.
 */
export function stringifyCatalogDelta(delta: CatalogDelta): string {
	const obj: Record<string, unknown> = { ...delta.extraFields };
	obj.deltaUpdate = operations(delta).map(({ kind, lo, hi }) => {
		let tracks: Record<string, unknown>[];
		switch (kind) {
			case "addTracks":
				tracks = delta.addTracks.slice(lo, hi).map(trackToJSON);
				break;
			case "removeTracks":
				tracks = delta.removeTracks.slice(lo, hi).map((ref) => {
					const entry: Record<string, unknown> = { ...ref.extraFields };
					if (ref.namespace) {
						entry.namespace = ref.namespace;
					}
					if (ref.name) {
						entry.name = ref.name;
					}
					return entry;
				});
				break;
			case "cloneTracks":
				tracks = delta.cloneTracks.slice(lo, hi).map(parentEntryToJSON);
				break;
			case "updateTracks":
				tracks = delta.updateTracks.slice(lo, hi).map(parentEntryToJSON);
				break;
		}
		return { op: KIND_TO_OP[kind], tracks };
	});
	if (delta.generatedAt !== undefined && delta.generatedAt !== null) {
		obj.generatedAt = delta.generatedAt;
	}
	return JSON.stringify(obj);
}
