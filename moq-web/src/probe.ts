/**
 * Result of a {@link Session.probe} or {@link Session.probeTargets} call.
 * Mirrors Go's `moqt.ProbeResult`.
 */
export interface ProbeResult {
	/** Measured (or target) bitrate in bits per second. 0 means unknown. */
	bitrate: number;
}
