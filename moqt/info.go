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
	orderedStr := "false"
	if pi.Ordered {
		orderedStr = "true"
	}
	return "{ priority: " + strconv.FormatUint(uint64(pi.Priority), 10) +
		", ordered: " + orderedStr +
		", max_latency_ms: " + strconv.FormatUint(pi.MaxLatency, 10) +
		", start_group: " + strconv.FormatUint(uint64(pi.StartGroup), 10) +
		", end_group: " + strconv.FormatUint(uint64(pi.EndGroup), 10) + " }"
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
