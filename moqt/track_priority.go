package moqt

// TrackPriority represents the delivery priority for a media track.
// Higher values indicate higher priority.
type TrackPriority byte

// urgencyFor maps a TrackPriority (0-255, higher = more important) to an
// RFC 9218 urgency (0-7, lower = more important). Zero (unset) maps to 3,
// matching quic-go's own default urgency so callers that never set Priority
// see no change in scheduling behavior.
func urgencyFor(p TrackPriority) (urgency int8, incremental bool) {
	if p == 0 {
		return 3, true
	}
	return int8(7 - int(p)*7/255), true
}
