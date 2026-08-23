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

// ⚡ Bolt: optimized allocation by replacing fmt.Sprintf with manual append and strconv.
func (sc SubscribeConfig) String() string {
	buf := make([]byte, 0, 128)
	buf = append(buf, "{ subscriber_priority: "...)
	buf = strconv.AppendUint(buf, uint64(sc.Priority), 10)
	buf = append(buf, ", ordered: "...)
	if sc.Ordered {
		buf = append(buf, "true"...)
	} else {
		buf = append(buf, "false"...)
	}
	buf = append(buf, ", max_latency_ms: "...)
	buf = strconv.AppendUint(buf, sc.MaxLatency, 10)
	buf = append(buf, ", start_group: "...)
	buf = strconv.AppendUint(buf, uint64(sc.StartGroup), 10)
	buf = append(buf, ", end_group: "...)
	buf = strconv.AppendUint(buf, uint64(sc.EndGroup), 10)
	buf = append(buf, " }"...)
	return string(buf)
}
