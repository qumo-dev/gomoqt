package moqt

import (
	"strconv"
)

// SubscribeConfig holds subscription parameters for a track.
// It describes the subscriber's requested delivery priority, ordering, latency,
// and group range.
type SubscribeConfig struct {
	Priority   TrackPriority
	Ordered    bool
	MaxLatency uint64
	StartGroup GroupSequence
	EndGroup   GroupSequence
}

// String returns a string representation of the subscribe config.
// Optimized using string concatenation and strconv instead of fmt.Sprintf for better performance.
func (sc SubscribeConfig) String() string {
	orderedStr := "false"
	if sc.Ordered {
		orderedStr = "true"
	}
	return "{ subscriber_priority: " + strconv.FormatUint(uint64(sc.Priority), 10) +
		", ordered: " + orderedStr +
		", max_latency_ms: " + strconv.FormatUint(sc.MaxLatency, 10) +
		", start_group: " + strconv.FormatUint(uint64(sc.StartGroup), 10) +
		", end_group: " + strconv.FormatUint(uint64(sc.EndGroup), 10) + " }"
}
