package moqt

import "fmt"

// PublishInfo holds publication parameters for a track.
// It is used to specify the delivery priority for the track.
// It is sent by the publisher to the server when a track is published,
// and is used by the server to determine how to deliver the track to subscribers.
type PublishInfo struct {
	Priority   TrackPriority
	Ordered    bool
	MaxLatency uint64
	StartGroup GroupSequence
	EndGroup   GroupSequence
}

func (pi PublishInfo) String() string {
	return fmt.Sprintf("{ priority: %d, ordered: %t, max_latency_ms: %d, start_group: %d, end_group: %d }", pi.Priority, pi.Ordered, pi.MaxLatency, pi.StartGroup, pi.EndGroup)
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
