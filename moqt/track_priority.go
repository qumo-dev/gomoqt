package moqt

// TrackPriority represents the delivery priority for a media track.
// Higher values indicate higher priority.
type TrackPriority byte

// urgencyFor maps a TrackPriority (0-255, higher = more important) to an
// RFC 9218 urgency (0-7, lower = more important) and the RFC 9218
// incremental bit.
//
// The urgency scale is linear across the whole range with no special case for
// zero: TrackPriority has no distinct "unset" representation, so 0 is simply
// the lowest priority and must land in the least urgent bucket. Special-casing
// it to quic-go's neutral default (3) would schedule Priority 0 ahead of
// Priority 1.
//
// Streams are always incremental. moq-lite-06 fixes group order within a
// track as newest first (§6.1.2); the transport can only serve streams of one
// urgency round-robin or oldest-stream-first, and round-robin is the one that
// does not hold the newest group behind the backlog.
func urgencyFor(p TrackPriority) (urgency int8, incremental bool) {
	return int8(7 - int(p)*7/255), true
}
