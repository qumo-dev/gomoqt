import { z } from "zod";
import { asRecord, zodSchemaError } from "./json.ts";

/** A `[groupId, objectId]` location reference within a MOQT track. */
export interface Location {
	groupId: number;
	objectId: number;
}

/** An entry in a media timeline mapping media time to a MOQT location. */
export interface MediaTimelineEntry {
	mediaTime: number;
	location: Location;
	wallclock: number;
}

/**
 * A record in an event timeline (draft-ietf-moq-msf-02 §8.2). Exactly one of
 * {@link wallclock} (wire key `T`), {@link location} (`L`) or
 * {@link mediaTime} (`M`) must be set, and {@link data} must be present.
 */
export interface EventTimelineRecord {
	wallclock?: number;
	location?: Location;
	mediaTime?: number;
	/** The event payload: any JSON value, including null, shaped by the track's eventType. */
	data?: unknown;
	extraFields?: Record<string, unknown>;
}

/**
 * A media timeline template for tracks with fixed-duration segments
 * (draft-ietf-moq-msf-02 §5.2.16, §7.4). Its wire form is the array
 * `[startMediaTime, deltaMediaTime, [groupId, objectId], [deltaGroup, deltaObject], startWallclock, deltaWallclock]`.
 */
export interface Template {
	/** Media presentation timestamp of the first entry. */
	startMediaTime: number;
	/** Constant interval between media timestamps, in milliseconds. */
	deltaMediaTime: number;
	/** MOQT Location of the first entry. */
	startLocation: Location;
	/** Constant interval between MOQT Locations. */
	deltaLocation: Location;
	/** Wallclock time of the first entry; 0 for VOD. */
	startWallclock: number;
	/** Constant interval between wallclock times, in milliseconds. */
	deltaWallclock: number;
}

const locationSchema = z.tuple([z.number(), z.number()]);

const mediaTimelineEntrySchema = z.tuple([z.number(), locationSchema, z.number()]);

const templateSchema = z.tuple([
	z.number().int(),
	z.number().int(),
	locationSchema,
	locationSchema,
	z.number().int(),
	z.number().int(),
]);

const eventTimelineRecordSchema = z.object({
	T: z.number().int().optional(),
	L: locationSchema.optional(),
	M: z.number().int().optional(),
});

/** Decode a `[groupId, objectId]` tuple into a {@link Location}. */
export function decodeLocation(value: unknown): Location {
	if (!Array.isArray(value) || value.length !== 2) {
		throw new Error("msf: location must contain exactly 2 items");
	}
	const parsed = locationSchema.safeParse(value);
	if (!parsed.success) {
		throw new Error("msf: location items must be numbers");
	}
	const [groupId, objectId] = parsed.data;
	return { groupId, objectId };
}

/** Encode a {@link Location} into a `[groupId, objectId]` tuple. */
export function encodeLocation(location: Location): [number, number] {
	return [location.groupId, location.objectId];
}

/** Decode a `[mediaTime, [groupId, objectId], wallclock]` tuple into a {@link MediaTimelineEntry}. */
export function decodeMediaTimelineEntry(value: unknown): MediaTimelineEntry {
	const parsed = mediaTimelineEntrySchema.safeParse(value);
	if (!parsed.success) {
		throw zodSchemaError(
			"msf: media timeline entry must contain numeric mediaTime, location, and wallclock",
			parsed.error,
		);
	}
	const [mediaTime, [groupId, objectId], wallclock] = parsed.data;
	return {
		mediaTime,
		location: { groupId, objectId },
		wallclock,
	};
}

/** Encode a {@link MediaTimelineEntry} into a `[mediaTime, [groupId, objectId], wallclock]` tuple. */
export function encodeMediaTimelineEntry(
	entry: MediaTimelineEntry,
): [number, [number, number], number] {
	return [entry.mediaTime, encodeLocation(entry.location), entry.wallclock];
}

/**
 * Check that the entries of an explicit-format media timeline are ordered by
 * media time (draft-ietf-moq-msf-02 §7.1.1). Group IDs are not checked: they
 * need not be sequential or increasing (§7.1).
 * @throws Error if an entry's media time precedes the previous entry's.
 */
export function validateMediaTimeline(entries: readonly MediaTimelineEntry[]): void {
	for (let i = 1; i < entries.length; i++) {
		const previous = entries[i - 1];
		const current = entries[i];
		if (
			previous !== undefined && current !== undefined &&
			current.mediaTime < previous.mediaTime
		) {
			throw new Error(
				`msf: media timeline entry ${i} has media time ${current.mediaTime} before the previous entry's ${previous.mediaTime}`,
			);
		}
	}
}

/** Decodes a template from its six-element array form. Internal. */
export function decodeTemplate(value: unknown): Template {
	if (!Array.isArray(value) || value.length !== 6) {
		throw new Error("msf: media timeline template must contain exactly 6 items");
	}
	const parsed = templateSchema.safeParse(value);
	if (!parsed.success) {
		throw zodSchemaError("msf: media timeline template", parsed.error);
	}
	const [startMediaTime, deltaMediaTime, start, delta, startWallclock, deltaWallclock] =
		parsed.data;
	return {
		startMediaTime,
		deltaMediaTime,
		startLocation: { groupId: start[0], objectId: start[1] },
		deltaLocation: { groupId: delta[0], objectId: delta[1] },
		startWallclock,
		deltaWallclock,
	};
}

/** Encodes a template into its six-element array form. Internal. */
export function encodeTemplate(
	template: Template,
): [number, number, [number, number], [number, number], number, number] {
	return [
		template.startMediaTime,
		template.deltaMediaTime,
		encodeLocation(template.startLocation),
		encodeLocation(template.deltaLocation),
		template.startWallclock,
		template.deltaWallclock,
	];
}

/**
 * Return the n-th (zero-based) timeline entry a template describes, by the
 * formulas of draft-ietf-moq-msf-02 §7.4.1.
 * @throws RangeError if n is not a non-negative integer.
 */
export function templateEntry(template: Template, n: number): MediaTimelineEntry {
	if (!Number.isInteger(n) || n < 0) {
		throw new RangeError(`msf: template entry index ${n} must be a non-negative integer`);
	}
	return {
		mediaTime: template.startMediaTime + n * template.deltaMediaTime,
		location: {
			groupId: template.startLocation.groupId + n * template.deltaLocation.groupId,
			objectId: template.startLocation.objectId + n * template.deltaLocation.objectId,
		},
		wallclock: template.startWallclock + n * template.deltaWallclock,
	};
}

/**
 * Validate that an {@link EventTimelineRecord} contains exactly one index
 * (`wallclock`, `location` or `mediaTime`) and a `data` field.
 * @throws Error if validation fails.
 */
export function validateEventTimelineRecord(record: EventTimelineRecord): void {
	const indexes = [record.wallclock, record.location, record.mediaTime]
		.filter((index) => index !== undefined).length;
	if (indexes !== 1) {
		throw new Error("msf: event timeline record must contain exactly one of T, L, or M");
	}
	if (record.data === undefined) {
		throw new Error("msf: event timeline record must contain data");
	}
}

/** Parse a raw JSON value into a validated {@link EventTimelineRecord}. */
export function parseEventTimelineRecord(value: unknown): EventTimelineRecord {
	const rawRecord = asRecord(value, "msf: event timeline record must be a JSON object");
	const { T, L, M, data, ...extra } = rawRecord;
	const parsed = eventTimelineRecordSchema.safeParse({ T, L, M });
	if (!parsed.success) {
		throw zodSchemaError("msf: event timeline record must be a JSON object", parsed.error);
	}
	const record: EventTimelineRecord = {};
	if (parsed.data.T !== undefined) {
		record.wallclock = parsed.data.T;
	}
	if (parsed.data.L !== undefined) {
		record.location = { groupId: parsed.data.L[0], objectId: parsed.data.L[1] };
	}
	if (parsed.data.M !== undefined) {
		record.mediaTime = parsed.data.M;
	}
	if ("data" in rawRecord) {
		record.data = data;
	}
	if (Object.keys(extra).length > 0) {
		record.extraFields = extra;
	}
	validateEventTimelineRecord(record);
	return record;
}

/** Encode an {@link EventTimelineRecord} into its JSON object form (keys `T`, `L`, `M`, `data`). */
export function encodeEventTimelineRecord(record: EventTimelineRecord): Record<string, unknown> {
	const obj: Record<string, unknown> = { ...record.extraFields };
	if (record.wallclock !== undefined) {
		obj.T = record.wallclock;
	}
	if (record.location !== undefined) {
		obj.L = encodeLocation(record.location);
	}
	if (record.mediaTime !== undefined) {
		obj.M = record.mediaTime;
	}
	if (record.data !== undefined) {
		obj.data = record.data;
	}
	return obj;
}
