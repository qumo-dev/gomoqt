package moqt

// TrackPriority represents the delivery priority for a media track.
// Higher values indicate higher priority.
type TrackPriority byte

// urgencyFor maps a TrackPriority (0-255, higher = more important) to an
// RFC 9218 urgency (0-7, lower = more important) and turns an ordering
// preference into the RFC 9218 incremental bit.
//
// The urgency scale is linear across the whole range with no special case for
// zero: TrackPriority has no distinct "unset" representation, so 0 is simply
// the lowest priority and must land in the least urgent bucket. Special-casing
// it to quic-go's neutral default (3) would schedule Priority 0 ahead of
// Priority 1.
//
// ordered inverts to incremental: streams of the same urgency are served
// round-robin when incremental, and in stream-ID order when not. Group streams
// are opened in ascending group order, so a non-incremental group stream asks
// the transport to finish older groups first. Callers with no ordering
// preference pass false and get fair round-robin progress.
func urgencyFor(p TrackPriority, ordered bool) (urgency int8, incremental bool) {
	return int8(7 - int(p)*7/255), !ordered
}
