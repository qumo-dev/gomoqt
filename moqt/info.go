package moqt

import "fmt"

// DefaultTimescale is the timescale used when a publisher does not specify
// one: 1000 units per second (millisecond timestamps).
const DefaultTimescale uint64 = 1000

// PublishInfo holds the immutable publisher properties of a track, as
// carried by the TRACK_INFO message. Every field is fixed for the lifetime
// of the track.
//
// moq-lite-06 removed the publisher's group-order preference: group order
// within a track is always newest first.
type PublishInfo struct {
	// Priority is the publisher's delivery priority for this track, used
	// only to resolve ties between subscriptions of equal subscriber priority.
	Priority TrackPriority
	// MaxAge is the maximum age, in milliseconds, that the publisher caches
	// a non-latest group past the arrival of a newer group (Publisher Max
	// Age). Zero means only the latest group is cached.
	MaxAge uint64
	// Timescale is the number of timestamp units per second for frame
	// timestamps on this track. Zero is treated as DefaultTimescale when
	// publishing; a received Timescale of zero is a protocol violation.
	Timescale uint64
}

func (pi PublishInfo) String() string {
	return fmt.Sprintf("{ priority: %d, max_age_ms: %d, timescale: %d }", pi.Priority, pi.MaxAge, pi.Timescale)
}

// timescaleOrDefault returns the configured timescale, substituting
// DefaultTimescale for the zero value so the wire value is always valid.
func (pi PublishInfo) timescaleOrDefault() uint64 {
	if pi.Timescale == 0 {
		return DefaultTimescale
	}
	return pi.Timescale
}
