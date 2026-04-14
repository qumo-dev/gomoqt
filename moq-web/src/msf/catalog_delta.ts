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
}).catchall(z.unknown());

const catalogDeltaSchema = z.object({
	deltaUpdate: z.literal(true),
	defaultNamespace: z.string().optional(),
	generatedAt: z.number().optional(),
	isComplete: z.boolean().optional(),
	addTracks: z.array(z.unknown()).optional(),
	removeTracks: z.array(z.unknown()).optional(),
	cloneTracks: z.array(z.unknown()).optional(),
}).catchall(z.unknown());

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
	const parentName = raw.parentName;
	const trackRecord = { ...raw };
	delete trackRecord.parentName;
	return {
		track: parseTrack(trackRecord),
		parentName,
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

	const deltaOpOrder: DeltaOperationKind[] = [];
	for (const key of Object.keys(rawRoot)) {
		if (key === "addTracks" || key === "removeTracks" || key === "cloneTracks") {
			deltaOpOrder.push(key);
		}
	}

	const addTracks = Array.isArray(root.addTracks) ? root.addTracks.map(parseTrack) : [];
	const removeTracks = Array.isArray(root.removeTracks)
		? root.removeTracks.map(parseTrackRef)
		: [];
	const cloneTracks = Array.isArray(root.cloneTracks)
		? root.cloneTracks.map(parseTrackClone)
		: [];

	const extraFields: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(root)) {
		if (
			key !== "deltaUpdate" &&
			key !== "defaultNamespace" &&
			key !== "generatedAt" &&
			key !== "isComplete" &&
			key !== "addTracks" &&
			key !== "removeTracks" &&
			key !== "cloneTracks"
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
					const parentId = trackId(
						{ namespace: clone.track.namespace, name: clone.parentName },
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

	validateCatalog(result);
	return result;
}

export function stringifyCatalogDelta(delta: CatalogDelta): string {
	const obj: Record<string, unknown> = {
		...(delta.extraFields ?? {}),
		deltaUpdate: true,
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
	if (delta.addTracks.length > 0) {
		obj.addTracks = delta.addTracks.map((track) => ({
			...(track.extraFields ?? {}),
			...track,
		}));
	}
	if (delta.removeTracks.length > 0) {
		obj.removeTracks = delta.removeTracks.map((track) => ({
			...(track.extraFields ?? {}),
			name: track.name,
			...(track.namespace ? { namespace: track.namespace } : {}),
		}));
	}
	if (delta.cloneTracks.length > 0) {
		obj.cloneTracks = delta.cloneTracks.map((clone) => ({
			...(clone.track.extraFields ?? {}),
			...clone.track,
			parentName: clone.parentName,
		}));
	}
	return JSON.stringify(obj);
}
