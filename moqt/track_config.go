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

func (sc SubscribeConfig) String() string {
	// ⚡ Bolt: Zero-allocation string formatting for SubscribeConfig.
	b := make([]byte, 0, 128)
	b = append(b, "{ subscriber_priority: "...)
	b = strconv.AppendUint(b, uint64(sc.Priority), 10)
	b = append(b, ", ordered: "...)
	if sc.Ordered {
		b = append(b, "true"...)
	} else {
		b = append(b, "false"...)
	}
	b = append(b, ", max_latency_ms: "...)
	b = strconv.AppendUint(b, sc.MaxLatency, 10)
	b = append(b, ", start_group: "...)
	b = strconv.AppendUint(b, uint64(sc.StartGroup), 10)
	b = append(b, ", end_group: "...)
	b = strconv.AppendUint(b, uint64(sc.EndGroup), 10)
	b = append(b, " }"...)
	return string(b)
}
