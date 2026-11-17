import { z } from "zod";
import { asRecord, decodeCatalogJSON, splitKnown, withoutNullish, zodSchemaError } from "./json.ts";
import { namespaceProblem, nameTextProblem } from "./names.ts";
import { packagingMetadataProblem } from "./track_fields.ts";
import { decodeTemplate, encodeTemplate, type Template } from "./timeline.ts";

// Any string, while editors still suggest the listed literals.
type OpenString = string & Record<never, never>;

/**
 * The MSF version of the catalogs this package produces and accepts
 * (draft-ietf-moq-msf-02 §5.1.1).
 */
export const CurrentVersion = "draft-02";

/**
 * Track packaging format (draft-ietf-moq-msf-02 §5.2.4). `"cmaf"` and
 * `"legacy"` are non-spec values kept for compatibility.
 */
export type Packaging =
	| "loc"
	| "mediatimeline"
	| "eventtimeline"
	| "moqlog"
	| "moqmetrics"
	| "catalog"
	| "cmaf"
	| "legacy"
	| OpenString;

/** Track semantic role (draft-ietf-moq-msf-02 §5.2.7). */
export type Role =
	| "video"
	| "audio"
	| "audiodescription"
	| "caption"
	| "subtitle"
	| "signlanguage"
	| "mediatimeline"
	| "eventtimeline"
	| "log"
	| "metrics"
	| "data"
	| OpenString;

/**
 * One entry of a track's `depends` (draft-ietf-moq-msf-02 §5.2.15): a string
 * names another track by its Track Name, an integer names a renderGroup.
 */
export type Dependency = string | number;

/** How an {@link InitDataRef}'s data is interpreted (draft-ietf-moq-msf-02 §5.1.7). */
export type InitDataType = "inline" | "track-property" | "object-property" | OpenString;

/**
 * A track's target jitter-buffer configuration in integer milliseconds
 * (draft-ietf-moq-msf-02 §5.2.10).
 */
export interface Buffers {
	target?: number;
	min?: number;
	max?: number;
	extraFields?: Record<string, unknown>;
}

/**
 * An embedded accessibility feature of a track, such as CEA-608/708
 * captions (draft-ietf-moq-msf-02 §5.2.44).
 */
export interface AccessibilityDescriptor {
	scheme?: string;
	value?: string;
	extraFields?: Record<string, unknown>;
}

/**
 * A single track entry in an MSF catalog (draft-ietf-moq-msf-02 §5.2).
 *
 * A field is present exactly when it holds a value: anything but undefined,
 * null or the empty string. Validation, serialization and the clone and
 * update overrides all read presence that way, and {@link parseTrack} reads
 * a JSON null as absent. An explicitly empty `namespace`, which §5.2.2 makes
 * invalid, is a parse error; omit it to inherit the catalog namespace. As a
 * consequence a clone or update entry cannot clear a field.
 *
 * Naming fields (namespace, name, depends names and the parent fields of
 * clone and update entries) map to MOQT bytes as their UTF-8 encoding, with
 * no normalization, and compare exactly (§11.2).
 */
export interface Track {
	/**
	 * `/`-delimited string of 1 to 32 non-empty Track Namespace Fields (see
	 * {@link splitNamespace}). When absent the catalog's default namespace is
	 * inherited; a declared namespace replaces it.
	 */
	namespace?: string;
	name?: string;
	packaging?: Packaging;
	/** Semicolon-separated key=value pairs (§5.2.5); see {@link packagingMetadataValue}. */
	packagingMetadata?: string;
	eventType?: string;
	role?: Role;
	isLive?: boolean;
	targetLatency?: number;
	buffers?: Buffers;
	label?: string;
	renderGroup?: number;
	altGroup?: number;
	/** The id of this track's {@link Catalog.initDataList} entry (§5.2.14). */
	initRef?: string;
	depends?: Dependency[];
	template?: Template;
	temporalId?: number;
	spatialId?: number;
	codec?: string;
	mimeType?: string;
	framerate?: number;
	timescale?: number;
	/** Maximum bitrate in bits per second (§5.2.23). */
	bitrate?: number;
	avgBitrate?: number;
	maxGopDuration?: number;
	maxGroupDuration?: number;
	width?: number;
	height?: number;
	samplerate?: number;
	channelConfig?: string;
	displayWidth?: number;
	displayHeight?: number;
	lang?: string;
	trackDuration?: number;
	/** MOQT endpoint URI of a publish track (§5.2.37). */
	connectionURI?: string;
	encryptionScheme?: string;
	/** Required when encryptionScheme is `"moq-secure-objects"` (§5.2.39). */
	cipherSuite?: string;
	keyId?: string;
	trackBaseKey?: string;
	authInfo?: Record<string, unknown>;
	accessibility?: AccessibilityDescriptor[];
	/** How far behind the live edge content stays accessible, in ms (§5.2.45). */
	accessibleWindowTime?: number;
	keyframeSpacing?: number;
	extraFields?: Record<string, unknown>;
}

/**
 * An entry in a catalog's Initialization Data List (draft-ietf-moq-msf-02
 * §5.1.7), referenced from tracks by {@link Track.initRef}.
 */
export interface InitDataRef {
	/** Unique-within-catalog id that tracks reference via `initRef`. */
	id: string;
	type?: InitDataType;
	/**
	 * Base64-encoded init data for `"inline"`; the hex type of the property
	 * carrying it for `"track-property"` and `"object-property"`.
	 */
	data?: string;
	extraFields?: Record<string, unknown>;
}

/**
 * An independent MSF catalog describing the available tracks for a
 * broadcast (draft-ietf-moq-msf-02).
 *
 * {@link defaultNamespace} is not serialized: it is the namespace implied by
 * the catalog track itself, which a receiver supplies, and resolves the
 * namespace of tracks that declare none.
 */
export interface Catalog {
	defaultNamespace?: string;
	/** {@link CurrentVersion} for catalogs this package accepts. */
	version: string;
	generatedAt?: number;
	isComplete?: boolean;
	tracks: Track[];
	/** Tracks the subscriber may publish to, such as logs or metrics (§5.1.5). */
	publishTracks?: Track[];
	/** Init payloads shared by reference (§5.1.7). */
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

const integer = z.number().int();

const buffersSchema = z.object({
	target: integer.nullish(),
	min: integer.nullish(),
	max: integer.nullish(),
}).catchall(z.unknown());

const accessibilitySchema = z.object({
	scheme: z.string().nullish(),
	value: z.string().nullish(),
}).catchall(z.unknown());

type TrackFieldKey = Exclude<keyof Track, "extraFields">;

// A JSON null is accepted for every field and read as absent.
const trackShape = {
	namespace: z.string().nullish(),
	name: z.string().nullish(),
	packaging: z.string().nullish(),
	packagingMetadata: z.string().nullish(),
	eventType: z.string().nullish(),
	role: z.string().nullish(),
	isLive: z.boolean().nullish(),
	targetLatency: integer.nullish(),
	buffers: buffersSchema.nullish(),
	label: z.string().nullish(),
	renderGroup: integer.nullish(),
	altGroup: integer.nullish(),
	initRef: z.string().nullish(),
	depends: z.array(z.union([z.string(), integer])).nullish(),
	template: z.unknown().optional(),
	temporalId: integer.nullish(),
	spatialId: integer.nullish(),
	codec: z.string().nullish(),
	mimeType: z.string().nullish(),
	framerate: integer.nullish(),
	timescale: integer.nullish(),
	bitrate: integer.nullish(),
	avgBitrate: integer.nullish(),
	maxGopDuration: integer.nullish(),
	maxGroupDuration: integer.nullish(),
	width: integer.nullish(),
	height: integer.nullish(),
	samplerate: integer.nullish(),
	channelConfig: z.string().nullish(),
	displayWidth: integer.nullish(),
	displayHeight: integer.nullish(),
	lang: z.string().nullish(),
	trackDuration: integer.nullish(),
	connectionURI: z.string().nullish(),
	encryptionScheme: z.string().nullish(),
	cipherSuite: z.string().nullish(),
	keyId: z.string().nullish(),
	trackBaseKey: z.string().nullish(),
	authInfo: z.record(z.string(), z.unknown()).nullish(),
	accessibility: z.array(accessibilitySchema).nullish(),
	accessibleWindowTime: integer.nullish(),
	keyframeSpacing: integer.nullish(),
} satisfies Record<TrackFieldKey, z.ZodType<unknown>>;

const trackSchema = z.object(trackShape);

/** Every field {@link Track} models, in serialization order. */
const TRACK_FIELD_KEYS = Object.keys(trackShape) as TrackFieldKey[];

const TRACK_FIELD_KEY_SET: ReadonlySet<string> = new Set(TRACK_FIELD_KEYS);

const CATALOG_KEYS: ReadonlySet<string> = new Set([
	"version",
	"generatedAt",
	"isComplete",
	"tracks",
	"publishTracks",
	"initDataList",
]);

const catalogSchema = z.object({
	version: z.string().nullish(),
	generatedAt: integer.nullish(),
	isComplete: z.boolean().nullish(),
	tracks: z.array(z.unknown()).nullish(),
	publishTracks: z.array(z.unknown()).nullish(),
	initDataList: z.array(z.unknown()).nullish(),
});

const initDataRefSchema = z.object({
	id: z.string().nullish(),
	type: z.string().nullish(),
	data: z.string().nullish(),
});

const INIT_DATA_REF_KEYS: ReadonlySet<string> = new Set(["id", "type", "data"]);

const BUFFERS_KEYS: ReadonlySet<string> = new Set(["target", "min", "max"]);

const ACCESSIBILITY_KEYS: ReadonlySet<string> = new Set(["scheme", "value"]);

/** The namespace a track resolves to when neither it nor the catalog has one. Internal. */
export const INHERITED_NAMESPACE = "\u0000catalog";

/** The message for an explicitly empty namespace (draft-ietf-moq-msf-02 §5.2.2). */
export const EMPTY_NAMESPACE_MESSAGE =
	"msf: namespace must not be empty; omit it to inherit the catalog namespace";

/** Reports whether a track field value counts as present. */
export function isPresent(value: unknown): boolean {
	return value !== undefined && value !== null && value !== "";
}

/** Clone a {@link Track}, creating independent copies of arrays/objects. */
export function cloneTrack(track: Track): Track {
	return structuredClone(track);
}

/** Clone a {@link Catalog}, creating independent copies of all tracks. */
export function cloneCatalog(catalog: Catalog): Catalog {
	return structuredClone(catalog);
}

function parseBuffers(raw: Record<string, unknown>): Buffers {
	const [known, extraFields] = splitKnown(raw, BUFFERS_KEYS);
	return { ...withoutNullish(known), ...(extraFields ? { extraFields } : {}) };
}

function parseAccessibility(raw: Record<string, unknown>): AccessibilityDescriptor {
	const [known, extraFields] = splitKnown(raw, ACCESSIBILITY_KEYS);
	return { ...withoutNullish(known), ...(extraFields ? { extraFields } : {}) };
}

/**
 * Parse a raw JSON track object into a strongly-typed {@link Track}. A null
 * field is absent; unknown fields are kept in {@link Track.extraFields}.
 * @param value - The parsed JSON value.
 * @throws Error if the value is not a valid track object, or declares an
 * empty namespace.
 */
export function parseTrack(value: unknown): Track {
	const rawRecord = asRecord(value, "msf: track must be a JSON object");
	const [knownRaw, extraFields] = splitKnown(rawRecord, TRACK_FIELD_KEY_SET);
	const parsed = trackSchema.safeParse(knownRaw);
	if (!parsed.success) {
		throw zodSchemaError("msf: track must be a JSON object", parsed.error);
	}
	if (parsed.data.namespace === "") {
		throw new Error(EMPTY_NAMESPACE_MESSAGE);
	}
	const { buffers, template, accessibility, ...fields } = withoutNullish(parsed.data);
	const track: Track = { ...fields };
	if (buffers !== undefined) {
		track.buffers = parseBuffers(buffers);
	}
	if (template !== undefined && template !== null) {
		track.template = decodeTemplate(template);
	}
	if (accessibility !== undefined) {
		track.accessibility = accessibility.map(parseAccessibility);
	}
	if (extraFields) {
		track.extraFields = extraFields;
	}
	return track;
}

function parseInitDataRef(value: unknown, index: number): InitDataRef {
	const where = `msf: initDataList[${index}] must be a JSON object`;
	const [knownRaw, extraFields] = splitKnown(asRecord(value, where), INIT_DATA_REF_KEYS);
	const parsed = initDataRefSchema.safeParse(knownRaw);
	if (!parsed.success) {
		throw zodSchemaError(where, parsed.error);
	}
	const { id, ...fields } = withoutNullish(parsed.data);
	const ref: InitDataRef = { id: id ?? "", ...fields };
	if (extraFields) {
		ref.extraFields = extraFields;
	}
	return ref;
}

/**
 * Parse a JSON catalog payload into a {@link Catalog}. Validation is
 * separate; see {@link validateCatalog}.
 * @param data - UTF-8 encoded bytes or a JSON string.
 * @throws Error if the payload is not a catalog object, contains the
 * delta-only deltaUpdate field, begins with a byte order mark, is not UTF-8,
 * or contains an unpaired surrogate (draft-ietf-moq-msf-02 §11.2.1).
 */
export function parseCatalog(data: string | Uint8Array): Catalog {
	const root = asRecord(decodeCatalogJSON(data), "msf: expected JSON object");
	if ("deltaUpdate" in root) {
		throw new Error("msf: delta catalog fields are not allowed in an independent catalog");
	}
	const [knownRaw, extraFields] = splitKnown(root, CATALOG_KEYS);
	const parsed = catalogSchema.safeParse(knownRaw);
	if (!parsed.success) {
		throw zodSchemaError("msf: expected JSON object", parsed.error);
	}
	const fields = withoutNullish(parsed.data);

	const catalog: Catalog = {
		version: fields.version ?? "",
		tracks: (fields.tracks ?? []).map(parseTrack),
	};
	if (fields.generatedAt !== undefined) {
		catalog.generatedAt = fields.generatedAt;
	}
	if (fields.isComplete === true) {
		catalog.isComplete = true;
	}
	if (fields.publishTracks !== undefined) {
		catalog.publishTracks = fields.publishTracks.map(parseTrack);
	}
	if (fields.initDataList !== undefined) {
		catalog.initDataList = fields.initDataList.map(parseInitDataRef);
	}
	if (extraFields) {
		catalog.extraFields = extraFields;
	}
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
	if (value.namespace) {
		return value.namespace;
	}
	if (defaultNamespace) {
		return defaultNamespace;
	}
	return INHERITED_NAMESPACE;
}

/** Return a composite identifier string for a track (`namespace|name`). */
export function trackId(
	value: { namespace?: string; name?: string },
	defaultNamespace?: string,
): string {
	return `${effectiveNamespace(value, defaultNamespace)}|${value.name ?? ""}`;
}

/** An unambiguous key for a Full Track Name. Internal. */
export function identityKey(namespace: string, name: string): string {
	return JSON.stringify([namespace, name]);
}

/** Returns the key of a track's resolved Full Track Name. Internal. */
export function trackKey(
	value: { namespace?: string; name?: string },
	defaultNamespace: string | undefined,
): string {
	return identityKey(effectiveNamespace(value, defaultNamespace), value.name ?? "");
}

/** Renders a Full Track Name for messages: `namespace/name`, or the bare name when inherited. */
export function displayName(namespace: string, name: string): string {
	if (namespace === "" || namespace === INHERITED_NAMESPACE) {
		return name;
	}
	return `${namespace}/${name}`;
}

/** Renders a track's resolved Full Track Name for messages. Internal. */
export function displayTrack(
	value: { namespace?: string; name?: string },
	defaultNamespace: string | undefined,
): string {
	return displayName(effectiveNamespace(value, defaultNamespace), value.name ?? "");
}

/** The catalog array a track is declared in. */
export type TrackList = "tracks" | "publishTracks";

// The rows of draft-ietf-moq-msf-02 §5.2.4.1 Table 6 for loc packaging.
const REQUIRED_LOC_FIELDS: ReadonlyMap<string, readonly TrackFieldKey[]> = new Map([
	["audio", ["codec", "bitrate", "samplerate", "channelConfig"]],
	["video", ["codec", "bitrate", "framerate"]],
	["audiodescription", ["codec", "samplerate", "channelConfig", "lang"]],
	["signlanguage", ["codec", "framerate"]],
]);

function dependencyProblem(dep: Dependency): string | undefined {
	if (typeof dep === "number") {
		return Number.isInteger(dep) ? undefined : "depends entry renderGroup must be an integer";
	}
	if (dep === "") {
		return "depends entry must not be an empty track name";
	}
	return nameTextProblem(dep);
}

/**
 * Checks the track-level rules of draft-ietf-moq-msf-02 for a track declared
 * in list and returns the problems, unprefixed. Cross-track rules are checked
 * by {@link validateCatalog}. Internal.
 */
export function trackProblems(track: Track, list: TrackList): string[] {
	const problems: string[] = [];
	if (!track.name) {
		problems.push("name is required and must not be empty");
	} else {
		const problem = nameTextProblem(track.name);
		if (problem !== undefined) {
			problems.push(`name: ${problem}`);
		}
	}
	if (track.namespace) {
		const problem = namespaceProblem(track.namespace);
		if (problem !== undefined) {
			problems.push(problem);
		}
	}
	if (!track.packaging) {
		problems.push("packaging is required");
	}
	if (track.isLive === undefined || track.isLive === null) {
		problems.push("isLive is required");
	}
	if (isPresent(track.trackDuration) && track.isLive === true) {
		problems.push("trackDuration must not be present when isLive is true");
	}
	if (isPresent(track.targetLatency) && isPresent(track.buffers)) {
		problems.push("targetLatency and buffers must not both be present");
	}
	if (track.packagingMetadata) {
		const problem = packagingMetadataProblem(track.packagingMetadata, track.packaging);
		if (problem !== undefined) {
			problems.push(problem);
		}
	}
	(track.depends ?? []).forEach((dep, i) => {
		const problem = dependencyProblem(dep);
		if (problem !== undefined) {
			problems.push(`depends[${i}]: ${problem}`);
		}
	});
	if (track.extraFields && "parentName" in track.extraFields) {
		problems.push("parentName must only appear in a clone or update operation");
	}
	if (track.extraFields && "parentNamespace" in track.extraFields) {
		problems.push("parentNamespace must only appear in a clone or update operation");
	}

	if (track.packaging === "eventtimeline") {
		if (!track.eventType) {
			problems.push("eventType is required for eventtimeline tracks");
		}
	} else if (track.eventType) {
		problems.push("eventType must only be set for eventtimeline tracks");
	}

	switch (track.packaging) {
		case "loc":
			for (const field of REQUIRED_LOC_FIELDS.get(track.role ?? "") ?? []) {
				if (!isPresent(track[field])) {
					problems.push(`${field} is required for loc tracks with role ${track.role}`);
				}
			}
			break;
		case "mediatimeline":
			// §7.2: depends lists what the timeline applies to. §7.1: a media
			// timeline uses the explicit format or a template, never both.
			if (!track.depends || track.depends.length === 0) {
				problems.push("mediatimeline tracks must declare depends");
			}
			if (isPresent(track.template)) {
				problems.push("mediatimeline tracks must not also carry a template");
			}
			break;
		case "moqlog":
			problems.push(...publishOnlyProblems(track, list, "log"));
			break;
		case "moqmetrics":
			problems.push(...publishOnlyProblems(track, list, "metrics"));
			break;
	}

	if (track.encryptionScheme === "moq-secure-objects" && !track.cipherSuite) {
		problems.push("cipherSuite is required when encryptionScheme is moq-secure-objects");
	}
	return problems;
}

// Log and metrics tracks (draft-ietf-moq-msf-02 §9, §10) are declared in
// publishTracks, with the matching role, and named by a level "0" to "7".
function publishOnlyProblems(track: Track, list: TrackList, role: Role): string[] {
	const problems: string[] = [];
	const packaging = track.packaging ?? "";
	if (list !== "publishTracks") {
		problems.push(`${packaging} tracks must be declared in publishTracks`);
	}
	if (track.role !== role) {
		problems.push(`${packaging} tracks must have role ${role}`);
	}
	if (!/^[0-7]$/.test(track.name ?? "")) {
		problems.push(`${packaging} track name must be a level from "0" to "7"`);
	}
	return problems;
}

function initDataRefProblem(ref: InitDataRef): string | undefined {
	switch (ref.type) {
		case undefined:
		case "":
		case "inline":
			return undefined;
		case "track-property":
		case "object-property":
			return initDataPropertyTypeOf(ref) === undefined
				? `data ${JSON.stringify(ref.data ?? "")} must be a property type in hex notation`
				: undefined;
		default:
			return `type ${
				JSON.stringify(ref.type)
			} must be "inline", "track-property" or "object-property"`;
	}
}

const MAX_UINT64 = (1n << 64n) - 1n;

function initDataPropertyTypeOf(ref: InitDataRef): bigint | undefined {
	if (ref.type !== "track-property" && ref.type !== "object-property") {
		return undefined;
	}
	const digits = (ref.data ?? "").replace(/^0[xX]/, "");
	if (!/^[0-9a-fA-F]+$/.test(digits)) {
		return undefined;
	}
	const value = BigInt(`0x${digits}`);
	return value <= MAX_UINT64 ? value : undefined;
}

/**
 * Return the property type an `"track-property"` or `"object-property"`
 * {@link InitDataRef} names: its data in hex notation, with or without a
 * `0x` prefix (draft-ietf-moq-msf-02 §5.1.7, §12.2).
 * @throws Error if the entry names no property or its data is not a 64-bit
 * hex number.
 */
export function initDataPropertyType(ref: InitDataRef): bigint {
	const value = initDataPropertyTypeOf(ref);
	if (value === undefined) {
		throw new Error(
			`msf: init data entry ${JSON.stringify(ref.id)} (type ${
				JSON.stringify(ref.type ?? "")
			}, data ${JSON.stringify(ref.data ?? "")}) does not name a property type in hex`,
		);
	}
	return value;
}

/**
 * Validate a {@link Catalog} according to the MSF draft-02 rules: the
 * version, the per-track rules including the required fields per
 * (packaging, role) of §5.2.4.1, unique Full Track Names across tracks and
 * publishTracks, the Initialization Data List (every initRef must resolve),
 * that every depends entry resolves (§5.2.15), and that the tracks of an
 * alternate group share accessibleWindowTime (§5.2.45).
 *
 * A catalog that uses variable substitution must be validated after
 * substitution; {@link resolveCatalogVariables} does so.
 * @throws {@link ValidationError} if any problems are found.
 */
export function validateCatalog(catalog: Catalog): void {
	const problems: string[] = [];
	if (!catalog.version) {
		problems.push("catalog version is required");
	} else if (catalog.version !== CurrentVersion) {
		problems.push(
			`unsupported catalog version ${JSON.stringify(catalog.version)}; want ${
				JSON.stringify(CurrentVersion)
			}`,
		);
	}
	const lists: [TrackList, Track[]][] = [
		["tracks", catalog.tracks],
		["publishTracks", catalog.publishTracks ?? []],
	];
	for (const [field, tracks] of lists) {
		tracks.forEach((track, i) => {
			for (const problem of trackProblems(track, field)) {
				problems.push(`${field}[${i}]: ${problem}`);
			}
		});
	}

	const seen = new Set<string>();
	for (const [field, tracks] of lists) {
		tracks.forEach((track, i) => {
			const key = trackKey(track, catalog.defaultNamespace);
			if (seen.has(key)) {
				problems.push(
					`${field}[${i}]: duplicate track identity ${
						JSON.stringify(displayTrack(track, catalog.defaultNamespace))
					}`,
				);
				return;
			}
			seen.add(key);
		});
	}

	const initIds = new Set<string>();
	(catalog.initDataList ?? []).forEach((ref, i) => {
		const problem = initDataRefProblem(ref);
		if (problem !== undefined) {
			problems.push(`initDataList[${i}]: ${problem}`);
		}
		if (!ref.id) {
			problems.push(`initDataList[${i}]: id is required`);
			return;
		}
		if (initIds.has(ref.id)) {
			problems.push(`initDataList[${i}]: duplicate init id ${JSON.stringify(ref.id)}`);
			return;
		}
		initIds.add(ref.id);
	});
	// An initRef must resolve even when the catalog declares no initDataList.
	for (const [field, tracks] of lists) {
		tracks.forEach((track, i) => {
			if (track.initRef && !initIds.has(track.initRef)) {
				problems.push(
					`${field}[${i}]: initRef ${
						JSON.stringify(track.initRef)
					} does not match any initDataList id`,
				);
			}
		});
	}

	problems.push(...dependencyProblems(lists));
	problems.push(...altGroupProblems(catalog.tracks));
	if (problems.length > 0) {
		throw new ValidationError(problems);
	}
}

// Every depends entry must resolve (draft-ietf-moq-msf-02 §5.2.15): a track
// name matches the Track Name of exactly one other track, a renderGroup the
// renderGroup of some track. tracks and publishTracks are both searched.
function dependencyProblems(lists: [TrackList, Track[]][]): string[] {
	const names = new Map<string, number>();
	const groups = new Set<number>();
	for (const [, tracks] of lists) {
		for (const track of tracks) {
			const name = track.name ?? "";
			names.set(name, (names.get(name) ?? 0) + 1);
			if (track.renderGroup !== undefined) {
				groups.add(track.renderGroup);
			}
		}
	}

	const problems: string[] = [];
	for (const [field, tracks] of lists) {
		tracks.forEach((track, i) => {
			(track.depends ?? []).forEach((dep, j) => {
				if (typeof dep === "number") {
					if (Number.isInteger(dep) && !groups.has(dep)) {
						problems.push(
							`${field}[${i}]: depends[${j}]: renderGroup ${dep} does not match any track`,
						);
					}
					return;
				}
				if (dep === "") {
					return; // reported by trackProblems
				}
				let others = names.get(dep) ?? 0;
				if (dep === (track.name ?? "")) {
					others--;
				}
				if (others === 0) {
					problems.push(
						`${field}[${i}]: depends[${j}]: track name ${
							JSON.stringify(dep)
						} does not match any other track`,
					);
				} else if (others > 1) {
					problems.push(
						`${field}[${i}]: depends[${j}]: track name ${
							JSON.stringify(dep)
						} matches ${others} tracks; it must match exactly one`,
					);
				}
			});
		});
	}
	return problems;
}

// The tracks of an alternate group carry the same accessibleWindowTime
// (draft-ietf-moq-msf-02 §5.2.45).
function altGroupProblems(tracks: Track[]): string[] {
	const windows = new Map<number, number | undefined>();
	const problems: string[] = [];
	tracks.forEach((track, i) => {
		if (track.altGroup === undefined || track.altGroup === null) {
			return;
		}
		const window = isPresent(track.accessibleWindowTime)
			? track.accessibleWindowTime
			: undefined;
		if (!windows.has(track.altGroup)) {
			windows.set(track.altGroup, window);
			return;
		}
		if (windows.get(track.altGroup) !== window) {
			problems.push(
				`tracks[${i}]: accessibleWindowTime must match the other tracks of altGroup ${track.altGroup}`,
			);
		}
	});
	return problems;
}

function withExtra(
	extraFields: Record<string, unknown> | undefined,
	fields: Record<string, unknown>,
): Record<string, unknown> {
	const obj: Record<string, unknown> = { ...extraFields };
	for (const [key, value] of Object.entries(fields)) {
		if (isPresent(value)) {
			obj[key] = value;
		}
	}
	return obj;
}

/** Returns the JSON object form of a track. Internal. */
export function trackToJSON(track: Track): Record<string, unknown> {
	const obj: Record<string, unknown> = { ...track.extraFields };
	for (const key of TRACK_FIELD_KEYS) {
		const value = track[key];
		if (!isPresent(value)) {
			continue;
		}
		switch (key) {
			case "buffers": {
				const { extraFields, ...fields } = track.buffers ?? {};
				obj.buffers = withExtra(extraFields, fields);
				break;
			}
			case "template":
				if (track.template !== undefined) {
					obj.template = encodeTemplate(track.template);
				}
				break;
			case "accessibility":
				obj.accessibility = (track.accessibility ?? []).map(({ extraFields, ...fields }) =>
					withExtra(extraFields, fields)
				);
				break;
			default:
				obj[key] = value;
		}
	}
	return obj;
}

/**
 * Returns base with every field override declares (see {@link Track} on
 * presence) replaced, and override's extraFields merged in. Internal.
 */
export function applyTrackOverrides(base: Track, override: Track): Track {
	const next = cloneTrack(base);
	for (const key of TRACK_FIELD_KEYS) {
		if (isPresent(override[key])) {
			copyField(next, override, key);
		}
	}
	if (override.extraFields) {
		next.extraFields = { ...next.extraFields, ...structuredClone(override.extraFields) };
	}
	return next;
}

function copyField<K extends TrackFieldKey>(target: Track, source: Track, key: K): void {
	target[key] = structuredClone(source[key]);
}

/**
 * Serialize a {@link Catalog} to its draft-02 JSON form. `tracks` is always
 * written (`[]` when empty), as the field is required, and `initDataList`
 * comes last, after the tracks (§5.1.7). {@link Catalog.defaultNamespace}
 * is not written.
 */
export function stringifyCatalog(catalog: Catalog): string {
	const obj: Record<string, unknown> = { ...catalog.extraFields };
	if (catalog.version) {
		obj.version = catalog.version;
	}
	if (catalog.generatedAt !== undefined && catalog.generatedAt !== null) {
		obj.generatedAt = catalog.generatedAt;
	}
	if (catalog.isComplete) {
		obj.isComplete = true;
	}
	obj.tracks = catalog.tracks.map(trackToJSON);
	if (catalog.publishTracks && catalog.publishTracks.length > 0) {
		obj.publishTracks = catalog.publishTracks.map(trackToJSON);
	}
	// Deleted first so that it is the last member even when an extra field held it.
	delete obj.initDataList;
	if (catalog.initDataList && catalog.initDataList.length > 0) {
		obj.initDataList = catalog.initDataList.map(({ extraFields, ...fields }) =>
			withExtra(extraFields, fields)
		);
	}
	return JSON.stringify(obj);
}
