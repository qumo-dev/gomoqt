package moqt

import (
	"strconv"
)

// PublishInfo holds publication metadata for a track.
// It describes delivery preferences such as priority, ordering, latency, and
// the group range that a publisher intends to serve.
type PublishInfo struct {
	Priority   TrackPriority
	Ordered    bool
	MaxLatency uint64
	StartGroup GroupSequence
	EndGroup   GroupSequence
}

// ⚡ Bolt: optimized allocation by replacing fmt.Sprintf with manual append and strconv.
func (pi PublishInfo) String() string {
	buf := make([]byte, 0, 128)
	buf = append(buf, "{ priority: "...)
	buf = strconv.AppendUint(buf, uint64(pi.Priority), 10)
	buf = append(buf, ", ordered: "...)
	if pi.Ordered {
		buf = append(buf, "true"...)
	} else {
		buf = append(buf, "false"...)
	}
	buf = append(buf, ", max_latency_ms: "...)
	buf = strconv.AppendUint(buf, pi.MaxLatency, 10)
	buf = append(buf, ", start_group: "...)
	buf = strconv.AppendUint(buf, uint64(pi.StartGroup), 10)
	buf = append(buf, ", end_group: "...)
	buf = strconv.AppendUint(buf, uint64(pi.EndGroup), 10)
	buf = append(buf, " }"...)
	return string(buf)
}

func ResolveTrackInfo(config SubscribeConfig, info PublishInfo) SubscribeConfig {
	return SubscribeConfig{
		Priority:   max(config.Priority, info.Priority),
		Ordered:    config.Ordered || info.Ordered,
		MaxLatency: max(config.MaxLatency, info.MaxLatency),
		StartGroup: max(config.StartGroup, info.StartGroup),
		EndGroup:   max(config.EndGroup, info.EndGroup),
	}
}
