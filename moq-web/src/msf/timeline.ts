import { asRecord, zodSchemaError } from "./catalog.ts";
import { z } from "zod";

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
 * A record in an event timeline. Exactly one of `t` (timestamp),
 * `l` (location), or `m` (media time) must be set alongside `data`.
 */
export interface EventTimelineRecord {
	t?: number;
	l?: Location;
	m?: number;
	data?: unknown;
	extraFields?: Record<string, unknown>;
}

const locationSchema = z.tuple([z.number(), z.number()]);

const mediaTimelineEntrySchema = z.tuple([z.number(), locationSchema, z.number()]);

const eventTimelineRecordSchema = z.object({
	t: z.number().optional(),
	l: locationSchema.optional(),
	m: z.number().optional(),
	data: z.unknown().optional(),
}).catchall(z.unknown());

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
 * Validate that an {@link EventTimelineRecord} contains exactly one timing
 * field (`t`, `l`, or `m`) and a `data` field.
 * @throws Error if validation fails.
 */
export function validateEventTimelineRecord(record: EventTimelineRecord): void {
	let count = 0;
	if (record.t !== undefined) {
		count++;
	}
	if (record.l !== undefined) {
		count++;
	}
	if (record.m !== undefined) {
		count++;
	}
	if (count !== 1) {
		throw new Error("msf: event timeline record must contain exactly one of t, l, or m");
	}
	if (record.data === undefined) {
		throw new Error("msf: event timeline record must contain data");
	}
}

/** Parse a raw JSON value into a validated {@link EventTimelineRecord}. */
export function parseEventTimelineRecord(value: unknown): EventTimelineRecord {
	const rawRecord = asRecord(value, "msf: event timeline record must be a JSON object");
	const parsed = eventTimelineRecordSchema.safeParse(rawRecord);
	if (!parsed.success) {
		throw zodSchemaError("msf: event timeline record must be a JSON object", parsed.error);
	}
	const raw = parsed.data;
	const extraFields: Record<string, unknown> = {};
	for (const [key, fieldValue] of Object.entries(raw)) {
		if (key !== "t" && key !== "l" && key !== "m" && key !== "data") {
			extraFields[key] = fieldValue;
		}
	}
	const record: EventTimelineRecord = {
		t: raw.t,
		l: raw.l !== undefined ? { groupId: raw.l[0], objectId: raw.l[1] } : undefined,
		m: raw.m,
		data: raw.data,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
	validateEventTimelineRecord(record);
	return record;
}
