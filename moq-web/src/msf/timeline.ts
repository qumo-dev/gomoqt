import { asRecord } from "./catalog.ts";

export interface Location {
	groupId: number;
	objectId: number;
}

export interface MediaTimelineEntry {
	mediaTime: number;
	location: Location;
	wallclock: number;
}

export interface EventTimelineRecord {
	t?: number;
	l?: Location;
	m?: number;
	data?: unknown;
	extraFields?: Record<string, unknown>;
}

export function decodeLocation(value: unknown): Location {
	if (!Array.isArray(value) || value.length !== 2) {
		throw new Error("msf: location must contain exactly 2 items");
	}
	const groupId = value[0];
	const objectId = value[1];
	if (typeof groupId !== "number" || typeof objectId !== "number") {
		throw new Error("msf: location items must be numbers");
	}
	return { groupId, objectId };
}

export function encodeLocation(location: Location): [number, number] {
	return [location.groupId, location.objectId];
}

export function decodeMediaTimelineEntry(value: unknown): MediaTimelineEntry {
	if (!Array.isArray(value) || value.length !== 3) {
		throw new Error("msf: media timeline entry must contain exactly 3 items");
	}
	const mediaTime = value[0];
	const location = value[1];
	const wallclock = value[2];
	if (typeof mediaTime !== "number" || typeof wallclock !== "number") {
		throw new Error("msf: media timeline entry must use numeric mediaTime and wallclock");
	}
	return {
		mediaTime,
		location: decodeLocation(location),
		wallclock,
	};
}

export function encodeMediaTimelineEntry(
	entry: MediaTimelineEntry,
): [number, [number, number], number] {
	return [entry.mediaTime, encodeLocation(entry.location), entry.wallclock];
}

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

export function parseEventTimelineRecord(value: unknown): EventTimelineRecord {
	const raw = asRecord(value, "msf: event timeline record must be a JSON object");
	const extraFields: Record<string, unknown> = {};
	for (const [key, fieldValue] of Object.entries(raw)) {
		if (key !== "t" && key !== "l" && key !== "m" && key !== "data") {
			extraFields[key] = fieldValue;
		}
	}
	const record: EventTimelineRecord = {
		t: typeof raw.t === "number" ? raw.t : undefined,
		l: raw.l !== undefined ? decodeLocation(raw.l) : undefined,
		m: typeof raw.m === "number" ? raw.m : undefined,
		data: raw.data,
		extraFields: Object.keys(extraFields).length > 0 ? extraFields : undefined,
	};
	validateEventTimelineRecord(record);
	return record;
}
