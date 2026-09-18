import { z } from "zod";
import { materializeInitData, resolveInitData } from "./init_data.ts";

type OpenString = string & Record<PropertyKey, never>;

/** Track packaging format. */
export type Packaging =
	| "loc"
	| "mediatimeline"
	| "eventtimeline"
	| "cmaf"
	| "legacy"
	| OpenString;

/** Track semantic role. */
export type Role =
	| "video"
	| "audio"
	| "audiodescription"
	| "caption"
	| "subtitle"
	| "signlanguage"
	| OpenString;

/** A single track entry in an MSF catalog. */
export interface Track {
	namespace?: string;
	name?: string;
	packaging?: Packaging;
	eventType?: string;
	role?: Role;
	isLive?: boolean;
	targetLatency?: number;
	label?: string;
	renderGroup?: number;
	altGroup?: number;
	/**
	 * The track's initialization data, base64-encoded. draft-ietf-moq-msf-01
	 * carries it out of line, as a catalog {@link Catalog.initDataList} entry
	 * referenced by {@link initRef}; {@link parseCatalog} resolves that
	 * reference into this field, and {@link stringifyCatalog} writes it back out
	 * as a list entry. It is never serialized inline.
	 */
	initData?: string;
	/** The id of this track's {@link Catalog.initDataList} entry. */
	initRef?: string;
	depends?: string[];
	temporalId?: number;
	spatialId?: number;
	codec?: string;
	mimeType?: string;
	framerate?: number;
	timescale?: number;
	bitrate?: number;
	width?: number;
	height?: number;
	samplerate?: number;
	channelConfig?: string;
	displayWidth?: number;
	displayHeight?: number;
	lang?: string;
	trackDuration?: number;
	extraFields?: Record<string, unknown>;
}

/**
 * An entry in a catalog's Initialization Data List (draft-ietf-moq-msf-01
 * §5.1.7), referenced from tracks by {@link Track.initRef}.
 */
export interface InitDataRef {
	/** Unique-within-catalog id that tracks reference via `initRef`. */
	id: string;
	/** How {@link data} is interpreted; only `"inline"` is defined. */
	type?: string;
	/** The init payload, base64-encoded when {@link type} is `"inline"`. */
	data?: string;
	extraFields?: Record<string, unknown>;
}

/**
 * An MSF catalog describing the available tracks for a broadcast.
 *
 * See [draft-ietf-moq-msf](https://datatracker.ietf.org/doc/draft-ietf-moq-msf/) for the wire format.
 */
export interface Catalog {
	defaultNamespace?: string;
	version: number;
	generatedAt?: number;
	isComplete?: boolean;
	tracks: Track[];
	/** Init payloads shared by reference (draft-ietf-moq-msf-01 §5.1.7). */
	initDataList?: InitDataRef[];
	extraFields?: Record<string, unknown>;
}

/** Error thrown when catalog or track validation fails. */
export class ValidationError extends Error {
	readonly problems: string[];

	constructor(problems: string[]) {
		super(
			problems.length === 0
				? "msf: validation failed"
				: `msf: validation failed: ${problems.join("; ")}`,
		);
		this.name = "ValidationError";
		this.problems = problems;
	}
}

const TRACK_KNOWN_KEYS = new Set<string>([
	"namespace",
	"name",
	"packaging",
	"eventType",
	"role",
	"isLive",
	"targetLatency",
	"label",
	"renderGroup",
	"altGroup",
	"initData",
	"initRef",
	"depends",
	"temporalId",
	"spatialId",
	"codec",
	"mimeType",
	"framerate",
	"timescale",
	"bitrate",
	"width",
	"height",
	"samplerate",
	"channelConfig",
	"displayWidth",
	"displayHeight",
	"lang",
	"trackDuration",
]);

const trackShape = {
	namespace: z.string().optional(),
	name: z.string().optional(),
	packaging: z.string().optional(),
	eventType: z.string().optional(),
	role: z.string().optional(),
	isLive: z.boolean().optional(),
	targetLatency: z.number().optional(),
	label: z.string().optional(),
	renderGroup: z.number().optional(),
	altGroup: z.number().optional(),
	initData: z.string().optional(),
	initRef: z.string().optional(),
	depends: z.array(z.string()).optional(),
	temporalId: z.number().optional(),
	spatialId: z.number().optional(),
	codec: z.string().optional(),
	mimeType: z.string().optional(),
	framerate: z.number().optional(),
	timescale: z.number().optional(),
	bitrate: z.number().optional(),
	width: z.number().optional(),
	height: z.number().optional(),
	samplerate: z.number().optional(),
	channelConfig: z.string().optional(),
	displayWidth: z.number().optional(),
	displayHeight: z.number().optional(),
	lang: z.string().optional(),
	trackDuration: z.number().optional(),
} satisfies Record<string, z.ZodType<unknown>>;

const trackSchema = z.object(trackShape).catchall(z.unknown());

const catalogSchema = z.object({
	defaultNamespace: z.string().optional(),
	version: z.number().optional(),
	generatedAt: z.number().optional(),
	isComplete: z.boolean().optional(),
	tracks: z.array(trackSchema).optional(),
	initDataList: z.array(z.unknown()).optional(),
}).catchall(z.unknown());

const initDataRefSchema = z.object({
	id: z.string(),
	type: z.string().optional(),
	data: z.string().optional(),
}).catchall(z.unknown());

export function zodSchemaError(prefix: string, error: z.ZodError): Error {
	const issue = error.issues[0];
	if (!issue) {
		return new Error(prefix);
	}
	const path = issue.path.length > 0 ? ` at ${issue.path.join(".")}` : "";
	return new Error(`${prefix}${path}: ${issue.message}`);
}

export function asRecord(value: unknown, errorMessage: string): Record<string, unknown> {
	if (typeof value !== "object" || value === null || Array.isArray(value)) {
		throw new Error(errorMessage);
	}
	return value as Record<string, unknown>;
}

export function decodeText(data: string | Uint8Array): string {
	if (typeof data === "string") {
		return data;
	}
	return new TextDecoder().decode(data);
}

/** Clone a {@link Track}, creating independent copies of arrays/objects. */
export function cloneTrack(track: Track): Track {
	return {
		...track,
		depends: track.depends ? [...track.depends] : undefined,
		extraFields: track.extraFields ? { ...track.extraFields } : undefined,
	};
}

/** Clone a {@link Catalog}, creating independent copies of all tracks. */
export function cloneCatalog(catalog: Catalog): Catalog {
	return {
		...catalog,
		tracks: catalog.tracks.map(cloneTrack),
		initDataList: catalog.initDataList?.map((ref) => ({
			...ref,
			extraFields: ref.extraFields ? { ...ref.extraFields } : undefined,
		})),
		extraFields: catalog.extraFields ? { ...catalog.extraFields } : undefined,
	};
}

function trackExtraFields(raw: Record<string, unknown>): Record<string, unknown> {
	const extra: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(raw)) {
		if (!TRACK_KNOWN_KEYS.has(key)) {
			extra[key] = value;
		}
	}
	return extra;
}

/**
 * Parse a raw JSON track object into a strongly-typed {@link Track}.
 * @param value - The parsed JSON value.
 * @throws Error if the value is not a valid track object.
 */
export function parseTrack(value: unknown): Track {
	const rawRecord = asRecord(value, "msf: track must be a JSON object");
	const raw = trackSchema.safeParse(rawRecord);
	if (!raw.success) {
		throw zodSchemaError("msf: track must be a JSON object", raw.error);
	}
	const parsed = raw.data;
	const extraFields = trackExtraFields(parsed);

	return {
		namespace: parsed.namespace,
		name: parsed.name,
		packaging: parsed.packaging as Packaging | undefined,
		eventType: parsed.eventType,
		role: parsed.role as Role | undefined,
		isLive: parsed.isLive,
		targetLatency: parsed.targetLatency,
		label: parsed.label,
		renderGroup: parsed.renderGroup,
		altGroup: parsed.altGroup,
		initData: parsed.initData,
		initRef: parsed.initRef,
		depends: parsed.depends,
		temporalId: parsed.temporalId,
		spatialId: parsed.spatialId,
		codec: parsed.codec,
		mimeType: parsed.mimeType,
		framerate: parsed.framerate,
		timescale: parsed.timescale,
		bitrate: parsed.bitrate,
		width: parsed.width,
		height: parsed.height,
		samplerate: parsed.samplerate,
		channelConfig: parsed.channelConfig,
		displayWidth: parsed.displayWidth,
		displayHeight: parsed.displayHeight,
		lang: parsed.lang,
		trackDuration: parsed.trackDuration,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
}

function parseInitDataRef(value: unknown, index: number): InitDataRef {
	const where = `msf: initDataList[${index}] must be a JSON object with a string id`;
	const parsed = initDataRefSchema.safeParse(asRecord(value, where));
	if (!parsed.success) {
		throw zodSchemaError(where, parsed.error);
	}
	const raw = parsed.data;
	const extra: Record<string, unknown> = {};
	for (const [key, field] of Object.entries(raw)) {
		if (key !== "id" && key !== "type" && key !== "data") {
			extra[key] = field;
		}
	}
	return {
		id: raw.id,
		type: raw.type,
		data: raw.data,
		extraFields: Object.keys(extra).length > 0 ? extra : undefined,
	};
}

/**
 * Parse a JSON catalog payload into a {@link Catalog}.
 * @param data - UTF-8 encoded bytes or a JSON string.
 * @throws Error if the payload is invalid or contains delta fields.
 */
export function parseCatalog(data: string | Uint8Array): Catalog {
	const parsed = catalogSchema.safeParse(JSON.parse(decodeText(data)));
	if (!parsed.success) {
		throw zodSchemaError("msf: expected JSON object", parsed.error);
	}
	const root = parsed.data;
	if (
		"deltaUpdate" in root ||
		"addTracks" in root ||
		"removeTracks" in root ||
		"cloneTracks" in root
	) {
		throw new Error("msf: delta catalog fields are not allowed in an independent catalog");
	}

	const tracksRaw = Array.isArray(root.tracks) ? root.tracks : [];
	const extraFields: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(root)) {
		if (
			key !== "defaultNamespace" &&
			key !== "version" &&
			key !== "generatedAt" &&
			key !== "isComplete" &&
			key !== "tracks" &&
			key !== "initDataList"
		) {
			extraFields[key] = value;
		}
	}

	const catalog: Catalog = {
		defaultNamespace: root.defaultNamespace,
		version: root.version ?? 0,
		generatedAt: root.generatedAt,
		isComplete: root.isComplete === true,
		tracks: tracksRaw.map(parseTrack),
		initDataList: Array.isArray(root.initDataList)
			? root.initDataList.map(parseInitDataRef)
			: undefined,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
	resolveInitData(catalog);
	return catalog;
}

/**
 * Return the effective namespace for a track, falling back to
 * `defaultNamespace` and then `"\0catalog"`.
 */
export function effectiveNamespace(
	value: { namespace?: string },
	defaultNamespace?: string,
): string {
	if (value.namespace && value.namespace.length > 0) {
		return value.namespace;
	}
	if (defaultNamespace && defaultNamespace.length > 0) {
		return defaultNamespace;
	}
	return "\u0000catalog";
}

/** Return a composite identifier string for a track (`namespace|name`). */
export function trackId(
	value: { namespace?: string; name?: string },
	defaultNamespace?: string,
): string {
	return `${effectiveNamespace(value, defaultNamespace)}|${value.name ?? ""}`;
}

/**
 * Validate a single track entry and return a list of problems.
 * @returns An array of human-readable problem descriptions (empty when valid).
 */
export function validateTrack(track: Track, path: string): string[] {
	const problems: string[] = [];
	if (!track.name) {
		problems.push(`${path}: name is required`);
	}
	if (!track.packaging) {
		problems.push(`${path}: packaging is required`);
	}
	if (track.trackDuration !== undefined && track.isLive === true) {
		problems.push(`${path}: trackDuration must not be present when isLive is true`);
	}

	switch (track.packaging) {
		case "eventtimeline":
			if (!track.eventType) {
				problems.push(`${path}: eventType is required for eventtimeline tracks`);
			}
			if (track.mimeType !== "application/json") {
				problems.push(`${path}: eventtimeline tracks must use mimeType application/json`);
			}
			if (!track.depends || track.depends.length === 0) {
				problems.push(`${path}: eventtimeline tracks must declare depends`);
			}
			break;
		case "mediatimeline":
			if (track.eventType) {
				problems.push(`${path}: eventType must not be set for mediatimeline tracks`);
			}
			if (track.mimeType !== "application/json") {
				problems.push(`${path}: mediatimeline tracks must use mimeType application/json`);
			}
			if (!track.depends || track.depends.length === 0) {
				problems.push(`${path}: mediatimeline tracks must declare depends`);
			}
			break;
		case "loc":
			if (track.eventType) {
				problems.push(`${path}: eventType must not be set for loc tracks`);
			}
			if (track.isLive === undefined) {
				problems.push(`${path}: isLive is required for loc tracks`);
			}
			break;
		default:
			if (track.eventType) {
				problems.push(`${path}: eventType must only be set for eventtimeline tracks`);
			}
	}

	return problems;
}

/**
 * Validate a {@link Catalog} according to MSF rules.
 * @throws {@link ValidationError} if any problems are found.
 */
export function validateCatalog(catalog: Catalog): void {
	const problems: string[] = [];
	if (!catalog.version) {
		problems.push("catalog version is required");
	}
	for (let i = 0; i < catalog.tracks.length; i++) {
		problems.push(...validateTrack(catalog.tracks[i]!, `tracks[${i}]`));
	}
	const seen = new Set<string>();
	for (let i = 0; i < catalog.tracks.length; i++) {
		const id = trackId(catalog.tracks[i]!, catalog.defaultNamespace);
		if (seen.has(id)) {
			problems.push(`tracks[${i}]: duplicate track identity ${JSON.stringify(id)}`);
			continue;
		}
		seen.add(id);
	}
	problems.push(...validateInitData(catalog));
	if (problems.length > 0) {
		throw new ValidationError(problems);
	}
}

// validateInitData checks the Initialization Data List: every entry has a
// unique id and the "inline" type, and every track reference resolves
// (draft-ietf-moq-msf-01 §5.1.7/§5.2.13). A track that carries its own
// initData needs no entry: stringifyCatalog writes one for it.
function validateInitData(catalog: Catalog): string[] {
	const problems: string[] = [];
	const ids = new Set<string>();
	const list = catalog.initDataList ?? [];
	for (let i = 0; i < list.length; i++) {
		const ref = list[i]!;
		if (ref.type !== undefined && ref.type !== "inline") {
			problems.push(`initDataList[${i}]: type must be "inline"`);
		}
		if (!ref.id) {
			problems.push(`initDataList[${i}]: id is required`);
			continue;
		}
		if (ids.has(ref.id)) {
			problems.push(`initDataList[${i}]: duplicate init id ${JSON.stringify(ref.id)}`);
			continue;
		}
		ids.add(ref.id);
	}
	for (let i = 0; i < catalog.tracks.length; i++) {
		const track = catalog.tracks[i]!;
		if (track.initRef && track.initData === undefined && !ids.has(track.initRef)) {
			problems.push(
				`tracks[${i}]: initRef ${
					JSON.stringify(track.initRef)
				} does not match any initDataList id`,
			);
		}
	}
	return problems;
}

/**
 * Serialize a {@link Catalog} to a JSON string.
 */
export function stringifyCatalog(catalog: Catalog): string {
	const obj: Record<string, unknown> = {
		...(catalog.extraFields ?? {}),
	};
	if (catalog.version !== 0) {
		obj.version = catalog.version;
	}
	if (catalog.defaultNamespace !== undefined) {
		obj.defaultNamespace = catalog.defaultNamespace;
	}
	if (catalog.generatedAt !== undefined) {
		obj.generatedAt = catalog.generatedAt;
	}
	if (catalog.isComplete) {
		obj.isComplete = true;
	}
	const { initDataList, initRefs } = materializeInitData(catalog);
	if (initDataList !== undefined) {
		obj.initDataList = initDataList.map(({ extraFields, ...ref }) => ({
			...(extraFields ?? {}),
			...ref,
		}));
	}
	if (catalog.tracks.length > 0) {
		// initData is never written inline: msf-01 carries it only through the
		// list above. extraFields is spread in, not written as its own key.
		obj.tracks = catalog.tracks.map(({ extraFields, initData: _, ...track }, i) => ({
			...(extraFields ?? {}),
			...track,
			initRef: initRefs[i],
		}));
	}
	return JSON.stringify(obj);
}
