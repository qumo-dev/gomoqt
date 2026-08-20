package moqt

import "strconv"

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

func (pi PublishInfo) String() string {
	// ⚡ Bolt: Zero-allocation string formatting for PublishInfo.
	b := make([]byte, 0, 128)
	b = append(b, "{ priority: "...)
	b = strconv.AppendUint(b, uint64(pi.Priority), 10)
	b = append(b, ", ordered: "...)
	if pi.Ordered {
		b = append(b, "true"...)
	} else {
		b = append(b, "false"...)
	}
	b = append(b, ", max_latency_ms: "...)
	b = strconv.AppendUint(b, pi.MaxLatency, 10)
	b = append(b, ", start_group: "...)
	b = strconv.AppendUint(b, uint64(pi.StartGroup), 10)
	b = append(b, ", end_group: "...)
	b = strconv.AppendUint(b, uint64(pi.EndGroup), 10)
	b = append(b, " }"...)
	return string(b)
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
