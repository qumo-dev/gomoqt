package moqt

import (
	"fmt"
)

// TrackConfig holds subscription parameters for a track. It is used to
// specify the delivery priority for the track.
type TrackConfig struct {
	TrackPriority TrackPriority
	Ordered       bool
	MaxLatencyMs  uint64
	StartGroup    GroupSequence
	EndGroup      GroupSequence
}

func (sc TrackConfig) String() string {
	return fmt.Sprintf("{ track_priority: %d, ordered: %t, max_latency_ms: %d, start_group: %d, end_group: %d }", sc.TrackPriority, sc.Ordered, sc.MaxLatencyMs, sc.StartGroup, sc.EndGroup)
}
