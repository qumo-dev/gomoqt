import type { TrackPriority } from "./alias.ts";

/**
 * The timescale used when a publisher does not specify one:
 * 1000 units per second (millisecond timestamps).
 */
export const DEFAULT_TIMESCALE = 1000;

/**
 * The immutable publisher properties of a track, carried by the TRACK_INFO
 * message. Every field is fixed for the lifetime of the track. moq-lite-06
 * removed the group-order preference: group order is always newest first.
 */
export interface Info {
	/** Publisher priority for this track, used only to resolve ties. */
	priority: TrackPriority;
	/**
	 * Publisher Max Age: the maximum age, in milliseconds, that the publisher
	 * caches a non-latest group past the arrival of a newer group.
	 */
	maxAge: number;
	/**
	 * Number of timestamp units per second for frame timestamps on this track.
	 * Always non-zero on the wire; zero is treated as {@link DEFAULT_TIMESCALE}
	 * when publishing.
	 */
	timescale: number;
}
