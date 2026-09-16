package moqt

import (
	"fmt"
)

// SubscribeConfig holds subscription parameters for a track.
// It describes the subscriber's requested delivery priority, ordering, latency,
// and group range.
type SubscribeConfig struct {
	Priority TrackPriority

	// Ordered asks the publisher to favor ascending group delivery. It is a
	// scheduling hint carried down to the transport, not a guarantee: groups
	// are still opened as soon as they are produced and their frames are
	// still written concurrently.
	Ordered bool

	MaxLatency uint64
	StartGroup GroupSequence
	EndGroup   GroupSequence
}

func (sc SubscribeConfig) String() string {
	return fmt.Sprintf("{ subscriber_priority: %d, ordered: %t, max_latency_ms: %d, start_group: %d, end_group: %d }", sc.Priority, sc.Ordered, sc.MaxLatency, sc.StartGroup, sc.EndGroup)
}
