import type { TrackPriority } from "./alias.ts";

/** Publisher-side track information sent back to the subscriber in a SUBSCRIBE_OK. */
export interface Info {
	/** Publisher priority for this track. */
	priority: TrackPriority;
	/** Whether groups must be delivered in order. */
	ordered: boolean;
	/** Maximum acceptable latency in milliseconds. */
	maxLatency: number;
	/** First group sequence available. */
	startGroup: number;
	/** Last group sequence available (0 means unbounded). */
	endGroup: number;
}
