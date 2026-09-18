import {
	asRecord,
	type Catalog,
	cloneCatalog,
	cloneTrack,
	decodeText,
	parseTrack,
	type Track,
	trackId,
	validateCatalog,
	validateTrack,
	ValidationError,
	zodSchemaError,
} from "./catalog.ts";
import { z } from "zod";
import { resolveInitData } from "./init_data.ts";

/** Discriminator for the three delta operations. */
export type DeltaOperationKind = "addTracks" | "removeTracks" | "cloneTracks";

/** Identifies a track for removal in a delta update. */
export interface TrackRef {
	namespace?: string;
	name?: string;
	extraFields?: Record<string, unknown>;
}

/** A track derived from a parent track via field overrides. */
export interface TrackClone {
	track: Track;
	parentName?: string;
	/** Namespace of the parent track, when it differs from the clone's. */
	parentNamespace?: string;
}

/**
 * An MSF delta update that adds, removes, or clones tracks in an existing
 * {@link Catalog}.
 */
export interface CatalogDelta {
	defaultNamespace?: string;
	generatedAt?: number;
	isComplete?: boolean;
	addTracks: Track[];
	removeTracks: TrackRef[];
	cloneTracks: TrackClone[];
	extraFields?: Record<string, unknown>;
	deltaOpOrder?: DeltaOperationKind[];
}

const trackRefSchema = z.object({
	namespace: z.string().optional(),
	name: z.string().optional(),
}).catchall(z.unknown());

const trackCloneSchema = z.object({
	parentName: z.string().optional(),
	parentNamespace: z.string().optional(),
}).catchall(z.unknown());

// draft-ietf-moq-msf-01: "deltaUpdate" is an array of {op, tracks} objects.
const deltaOperationSchema = z.object({
	op: z.string(),
	tracks: z.array(z.unknown()),
}).catchall(z.unknown());

const catalogDeltaSchema = z.object({
	deltaUpdate: z.array(deltaOperationSchema),
	defaultNamespace: z.string().optional(),
	generatedAt: z.number().optional(),
	isComplete: z.boolean().optional(),
}).catchall(z.unknown());

// Wire op name <-> in-memory operation kind.
const OP_TO_KIND: Record<string, DeltaOperationKind> = {
	add: "addTracks",
	remove: "removeTracks",
	clone: "cloneTracks",
};
const KIND_TO_OP: Record<DeltaOperationKind, string> = {
	addTracks: "add",
	removeTracks: "remove",
	cloneTracks: "clone",
};

function parseTrackRef(value: unknown): TrackRef {
	const rawRecord = asRecord(value, "msf: remove track reference must be a JSON object");
	const parsed = trackRefSchema.safeParse(rawRecord);
	if (!parsed.success) {
		throw zodSchemaError("msf: remove track reference must be a JSON object", parsed.error);
	}
	const raw = parsed.data;
	const extra: Record<string, unknown> = {};
	for (const [key, fieldValue] of Object.entries(raw)) {
		if (key !== "namespace" && key !== "name") {
			extra[key] = fieldValue;
		}
	}
	return {
		namespace: raw.namespace,
		name: raw.name,
		extraFields: Object.keys(extra).length > 0 ? extra : undefined,
	};
}

function parseTrackClone(value: unknown): TrackClone {
	const rawRecord = asRecord(value, "msf: clone track entry must be a JSON object");
	const parsed = trackCloneSchema.safeParse(rawRecord);
	if (!parsed.success) {
		throw zodSchemaError("msf: clone track entry must be a JSON object", parsed.error);
	}
	const raw = parsed.data;
	const { parentName, parentNamespace, ...trackRecord } = raw;
	return {
		track: parseTrack(trackRecord),
		parentName,
		parentNamespace,
	};
}

/**
 * Parse a JSON delta-catalog payload into a {@link CatalogDelta}.
 * @param data - UTF-8 encoded bytes or a JSON string.
 * @throws Error if the payload is invalid or contains independent catalog fields.
 */
export function parseCatalogDelta(data: string | Uint8Array): CatalogDelta {
	const decoded = JSON.parse(decodeText(data));
	const rawRoot = asRecord(decoded, "msf: expected JSON object");
	const parsed = catalogDeltaSchema.safeParse(rawRoot);
	if (!parsed.success) {
		throw zodSchemaError("msf: expected JSON object", parsed.error);
	}
	const root = parsed.data;
	if ("version" in root || "tracks" in root) {
		throw new Error("msf: independent catalog fields are not allowed in a delta catalog");
	}

	// Repeated operations of one kind are legal in draft-01: their tracks are
	// appended, and only the first-seen order of each kind is recorded.
	const deltaOpOrder: DeltaOperationKind[] = [];
	const addTracks: Track[] = [];
	const removeTracks: TrackRef[] = [];
	const cloneTracks: TrackClone[] = [];
	for (const operation of root.deltaUpdate) {
		const kind = OP_TO_KIND[operation.op];
		if (kind === undefined) {
			throw new Error(`msf: unknown delta update op ${JSON.stringify(operation.op)}`);
		}
		if (!deltaOpOrder.includes(kind)) {
			deltaOpOrder.push(kind);
		}
		switch (kind) {
			case "addTracks":
				addTracks.push(...operation.tracks.map(parseTrack));
				break;
			case "removeTracks":
				removeTracks.push(...operation.tracks.map(parseTrackRef));
				break;
			case "cloneTracks":
				cloneTracks.push(...operation.tracks.map(parseTrackClone));
				break;
		}
	}

	const extraFields: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(root)) {
		if (
			key !== "deltaUpdate" &&
			key !== "defaultNamespace" &&
			key !== "generatedAt" &&
			key !== "isComplete"
		) {
			extraFields[key] = value;
		}
	}

	return {
		defaultNamespace: root.defaultNamespace,
		generatedAt: root.generatedAt,
		isComplete: root.isComplete === true,
		addTracks,
		removeTracks,
		cloneTracks,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
		deltaOpOrder,
	};
}

/**
 * Validate a {@link CatalogDelta} according to MSF rules.
 * @throws {@link ValidationError} if any problems are found.
 */
export function validateCatalogDelta(delta: CatalogDelta): void {
	const problems: string[] = [];
	if (
		delta.addTracks.length === 0 &&
		delta.removeTracks.length === 0 &&
		delta.cloneTracks.length === 0
	) {
		problems.push("delta catalog must contain addTracks, removeTracks, or cloneTracks");
	}
	for (let i = 0; i < delta.addTracks.length; i++) {
		problems.push(...validateTrack(delta.addTracks[i]!, `addTracks[${i}]`));
	}
	for (let i = 0; i < delta.removeTracks.length; i++) {
		const ref = delta.removeTracks[i]!;
		if (!ref.name) {
			problems.push(`removeTracks[${i}]: name is required`);
		}
		if (ref.extraFields && Object.keys(ref.extraFields).length > 0) {
			problems.push(
				`removeTracks[${i}]: remove track entries may contain only name and optional namespace`,
			);
		}
	}
	for (let i = 0; i < delta.cloneTracks.length; i++) {
		const clone = delta.cloneTracks[i]!;
		if (!clone.track.name) {
			problems.push(`cloneTracks[${i}]: name is required`);
		}
		if (!clone.parentName) {
			problems.push(`cloneTracks[${i}]: parentName is required for clone tracks`);
		}
	}
	if (problems.length > 0) {
		throw new ValidationError(problems);
	}
}

function applyTrackOverrides(base: Track, override: Track): Track {
	const next: Track = cloneTrack(base);
	for (const [key, value] of Object.entries(override)) {
		if (key === "extraFields") {
			continue;
		}
		if (value !== undefined) {
			if (key === "depends" && Array.isArray(value)) {
				// Clone array-typed depends so the override and resulting track do not share the same instance.
				(next as Record<string, unknown>)[key] = [...value];
			} else {
				(next as Record<string, unknown>)[key] = value;
			}
		}
	}
	// initData is resolved from initRef, so an overridden initRef invalidates
	// the payload inherited from the parent; resolveInitData refills it.
	if (override.initRef !== undefined && override.initData === undefined) {
		delete next.initData;
	}
	if (override.extraFields) {
		next.extraFields = {
			...(next.extraFields ?? {}),
			...override.extraFields,
		};
	}
	return next;
}

function hasInheritedNamespaceTracks(catalog: Catalog): boolean {
	return catalog.tracks.some((track) => !track.namespace);
}

/**
 * Apply a validated delta to a base catalog and return the resulting catalog.
 * @param baseCatalog - The current catalog state.
 * @param deltaCatalog - The delta to apply.
 * @throws Error if the delta cannot be safely applied.
 */
export function applyCatalogDelta(baseCatalog: Catalog, deltaCatalog: CatalogDelta): Catalog {
	validateCatalog(baseCatalog);
	validateCatalogDelta(deltaCatalog);

	const result = cloneCatalog(baseCatalog);
	if (
		deltaCatalog.defaultNamespace !== undefined &&
		deltaCatalog.defaultNamespace !== "" &&
		deltaCatalog.defaultNamespace !== result.defaultNamespace
	) {
		if (hasInheritedNamespaceTracks(result)) {
			throw new Error(
				"msf: cannot change default namespace when catalog contains tracks that inherit it",
			);
		}
		result.defaultNamespace = deltaCatalog.defaultNamespace;
	}
	if (deltaCatalog.generatedAt !== undefined) {
		result.generatedAt = deltaCatalog.generatedAt;
	}
	if (deltaCatalog.isComplete) {
		result.isComplete = true;
	}
	const mergedExtraFields = {
		...(result.extraFields ?? {}),
		...(deltaCatalog.extraFields ?? {}),
	};
	result.extraFields = Object.keys(mergedExtraFields).length > 0 ? mergedExtraFields : undefined;

	const order = deltaCatalog.deltaOpOrder && deltaCatalog.deltaOpOrder.length > 0
		? deltaCatalog.deltaOpOrder
		: [
			deltaCatalog.addTracks.length > 0 ? "addTracks" : undefined,
			deltaCatalog.removeTracks.length > 0 ? "removeTracks" : undefined,
			deltaCatalog.cloneTracks.length > 0 ? "cloneTracks" : undefined,
		].filter((v): v is DeltaOperationKind => v !== undefined);

	for (const op of order) {
		switch (op) {
			case "addTracks":
				for (const track of deltaCatalog.addTracks) {
					const id = trackId(track, result.defaultNamespace);
					if (
						result.tracks.some((candidate) =>
							trackId(candidate, result.defaultNamespace) === id
						)
					) {
						throw new Error(`msf: cannot add duplicate track ${JSON.stringify(id)}`);
					}
					result.tracks.push(cloneTrack(track));
				}
				break;
			case "removeTracks":
				for (const ref of deltaCatalog.removeTracks) {
					const id = trackId(ref, result.defaultNamespace);
					const index = result.tracks.findIndex(
						(track) => trackId(track, result.defaultNamespace) === id,
					);
					if (index < 0) {
						throw new Error(`msf: cannot remove unknown track ${JSON.stringify(id)}`);
					}
					result.tracks.splice(index, 1);
				}
				break;
			case "cloneTracks":
				for (const clone of deltaCatalog.cloneTracks) {
					// As in the Go package: the parent lives in parentNamespace when
					// given, else the catalog default -- not the clone's own namespace.
					const parentId = trackId(
						{ namespace: clone.parentNamespace, name: clone.parentName },
						result.defaultNamespace,
					);
					const parent = result.tracks.find(
						(track) => trackId(track, result.defaultNamespace) === parentId,
					);
					if (!parent) {
						throw new Error(
							`msf: cannot clone unknown parent track ${JSON.stringify(parentId)}`,
						);
					}
					const derived = applyTrackOverrides(parent, clone.track);
					if (!derived.name) {
						throw new Error(
							`msf: cloned track derived from ${
								JSON.stringify(parentId)
							} is missing name`,
						);
					}
					const id = trackId(derived, result.defaultNamespace);
					if (
						result.tracks.some((track) =>
							trackId(track, result.defaultNamespace) === id
						)
					) {
						throw new Error(
							`msf: cannot clone into duplicate track ${JSON.stringify(id)}`,
						);
					}
					result.tracks.push(derived);
				}
		}
	}

	resolveInitData(result);
	validateCatalog(result);
	return result;
}

/**
 * Serialize a {@link CatalogDelta} to its draft-ietf-moq-msf-01 JSON form: a
 * `deltaUpdate` array of `{op, tracks}` objects, in {@link CatalogDelta.deltaOpOrder}
 * when it is set.
 *
 * A delta has no Initialization Data List, so a track can only reference an
 * entry of the base catalog through `initRef`; its resolved `initData` is not
 * written.
 * @throws Error if a track carries `initData` without an `initRef`, since
 * that payload cannot be represented in a delta.
 */
export function stringifyCatalogDelta(delta: CatalogDelta): string {
	const obj: Record<string, unknown> = {
		...(delta.extraFields ?? {}),
	};
	if (delta.defaultNamespace !== undefined) {
		obj.defaultNamespace = delta.defaultNamespace;
	}
	if (delta.generatedAt !== undefined) {
		obj.generatedAt = delta.generatedAt;
	}
	if (delta.isComplete) {
		obj.isComplete = true;
	}

	const trackWire = (track: Track, where: string): Record<string, unknown> => {
		const { extraFields, initData, ...rest } = track;
		if (initData !== undefined && !rest.initRef) {
			throw new Error(
				`msf: ${where} carries initData, which a delta cannot represent; ` +
					"reference an initDataList entry of the base catalog with initRef, " +
					"or publish an independent catalog",
			);
		}
		return { ...(extraFields ?? {}), ...rest };
	};

	const order: DeltaOperationKind[] = delta.deltaOpOrder && delta.deltaOpOrder.length > 0
		? delta.deltaOpOrder
		: (["addTracks", "removeTracks", "cloneTracks"] as const).filter((kind) =>
			delta[kind].length > 0
		);
	obj.deltaUpdate = order.map((kind) => {
		let tracks: Record<string, unknown>[];
		switch (kind) {
			case "addTracks":
				tracks = delta.addTracks.map((track, i) => trackWire(track, `addTracks[${i}]`));
				break;
			case "removeTracks":
				tracks = delta.removeTracks.map((ref) => ({
					...(ref.extraFields ?? {}),
					name: ref.name,
					...(ref.namespace ? { namespace: ref.namespace } : {}),
				}));
				break;
			case "cloneTracks":
				tracks = delta.cloneTracks.map((clone, i) => ({
					...trackWire(clone.track, `cloneTracks[${i}]`),
					parentName: clone.parentName,
					...(clone.parentNamespace ? { parentNamespace: clone.parentNamespace } : {}),
				}));
				break;
		}
		return { op: KIND_TO_OP[kind], tracks };
	});
	return JSON.stringify(obj);
}
