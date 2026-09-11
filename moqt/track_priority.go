package moqt

// TrackPriority represents the delivery priority for a media track.
// Higher values indicate higher priority.
type TrackPriority byte

// urgencyFor maps a TrackPriority (0-255, higher = more important) to an
// RFC 9218 urgency (0-7, lower = more important) via a single linear scale,
// so ordering is preserved across the whole range including zero: since
// TrackPriority has no separate "unset" representation, 0 is the lowest
// priority (per the TrackPriority doc) and must map to the least urgent
// bucket (7), not to quic-go's neutral default urgency (3) as an earlier
// version of this function did — that inverted scheduling order between
// Priority 0 and Priority 1. Callers that never set Priority all share
// urgency 7 uniformly, so relative scheduling among them is unaffected.
func urgencyFor(p TrackPriority) (urgency int8, incremental bool) {
	return int8(7 - int(p)*7/255), true
}
