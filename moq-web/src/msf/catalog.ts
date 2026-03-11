export type Packaging =
	| "loc"
	| "mediatimeline"
	| "eventtimeline"
	| "cmaf"
	| "legacy"
	| (string & {});

export type Role =
	| "video"
	| "audio"
	| "audiodescription"
	| "caption"
	| "subtitle"
	| "signlanguage"
	| (string & {});

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
	initData?: string;
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

export interface Catalog {
	defaultNamespace?: string;
	version: number;
	generatedAt?: number;
	isComplete?: boolean;
	tracks: Track[];
	extraFields?: Record<string, unknown>;
}

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

export function cloneTrack(track: Track): Track {
	return {
		...track,
		depends: track.depends ? [...track.depends] : undefined,
		extraFields: track.extraFields ? { ...track.extraFields } : undefined,
	};
}

export function cloneCatalog(catalog: Catalog): Catalog {
	return {
		...catalog,
		tracks: catalog.tracks.map(cloneTrack),
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

export function parseTrack(value: unknown): Track {
	const raw = asRecord(value, "msf: track must be a JSON object");
	const extraFields = trackExtraFields(raw);
	const depends = Array.isArray(raw.depends)
		? raw.depends.filter((v): v is string => typeof v === "string")
		: undefined;

	return {
		namespace: typeof raw.namespace === "string" ? raw.namespace : undefined,
		name: typeof raw.name === "string" ? raw.name : undefined,
		packaging: typeof raw.packaging === "string" ? (raw.packaging as Packaging) : undefined,
		eventType: typeof raw.eventType === "string" ? raw.eventType : undefined,
		role: typeof raw.role === "string" ? (raw.role as Role) : undefined,
		isLive: typeof raw.isLive === "boolean" ? raw.isLive : undefined,
		targetLatency: typeof raw.targetLatency === "number" ? raw.targetLatency : undefined,
		label: typeof raw.label === "string" ? raw.label : undefined,
		renderGroup: typeof raw.renderGroup === "number" ? raw.renderGroup : undefined,
		altGroup: typeof raw.altGroup === "number" ? raw.altGroup : undefined,
		initData: typeof raw.initData === "string" ? raw.initData : undefined,
		depends,
		temporalId: typeof raw.temporalId === "number" ? raw.temporalId : undefined,
		spatialId: typeof raw.spatialId === "number" ? raw.spatialId : undefined,
		codec: typeof raw.codec === "string" ? raw.codec : undefined,
		mimeType: typeof raw.mimeType === "string" ? raw.mimeType : undefined,
		framerate: typeof raw.framerate === "number" ? raw.framerate : undefined,
		timescale: typeof raw.timescale === "number" ? raw.timescale : undefined,
		bitrate: typeof raw.bitrate === "number" ? raw.bitrate : undefined,
		width: typeof raw.width === "number" ? raw.width : undefined,
		height: typeof raw.height === "number" ? raw.height : undefined,
		samplerate: typeof raw.samplerate === "number" ? raw.samplerate : undefined,
		channelConfig: typeof raw.channelConfig === "string" ? raw.channelConfig : undefined,
		displayWidth: typeof raw.displayWidth === "number" ? raw.displayWidth : undefined,
		displayHeight: typeof raw.displayHeight === "number" ? raw.displayHeight : undefined,
		lang: typeof raw.lang === "string" ? raw.lang : undefined,
		trackDuration: typeof raw.trackDuration === "number" ? raw.trackDuration : undefined,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
}

export function parseCatalog(data: string | Uint8Array): Catalog {
	const root = asRecord(JSON.parse(decodeText(data)), "msf: expected JSON object");
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
			key !== "tracks"
		) {
			extraFields[key] = value;
		}
	}

	return {
		defaultNamespace: typeof root.defaultNamespace === "string"
			? root.defaultNamespace
			: undefined,
		version: typeof root.version === "number" ? root.version : 0,
		generatedAt: typeof root.generatedAt === "number" ? root.generatedAt : undefined,
		isComplete: root.isComplete === true,
		tracks: tracksRaw.map(parseTrack),
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
}

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

export function trackId(
	value: { namespace?: string; name?: string },
	defaultNamespace?: string,
): string {
	return `${effectiveNamespace(value, defaultNamespace)}|${value.name ?? ""}`;
}

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
	if (problems.length > 0) {
		throw new ValidationError(problems);
	}
}

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
	if (catalog.tracks.length > 0) {
		obj.tracks = catalog.tracks.map((track) => ({
			...(track.extraFields ?? {}),
			...track,
		}));
	}
	return JSON.stringify(obj);
}
