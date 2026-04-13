package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrackConfig(t *testing.T) {
	tests := map[string]struct {
		trackPriority TrackPriority
	}{
		"default values": {
			trackPriority: TrackPriority(0),
		},
		"mid priority": {
			trackPriority: TrackPriority(128),
		},
		"high priority": {
			trackPriority: TrackPriority(255),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config := SubscribeConfig{
				Priority: tt.trackPriority,
			}

			assert.Equal(t, tt.trackPriority, config.Priority)
		})
	}
}

func TestTrackConfig_ZeroValue(t *testing.T) {
	var config SubscribeConfig

	assert.Equal(t, TrackPriority(0), config.Priority)
}

func TestTrackConfig_Comparison(t *testing.T) {
	config1 := SubscribeConfig{
		Priority: TrackPriority(128),
	}

	config2 := SubscribeConfig{
		Priority: TrackPriority(128),
	}

	config3 := SubscribeConfig{
		Priority: TrackPriority(64),
	}

	assert.Equal(t, config1, config2)
	assert.NotEqual(t, config1, config3)
}

func TestSubscribeConfig_String(t *testing.T) {
	config := SubscribeConfig{
		Priority:   128,
		Ordered:    true,
		MaxLatency: 250,
		StartGroup: 5,
		EndGroup:   10,
	}

	result := config.String()
	assert.Contains(t, result, "subscriber_priority: 128")
	assert.Contains(t, result, "ordered: true")
	assert.Contains(t, result, "max_latency_ms: 250")
	assert.Contains(t, result, "start_group: 5")
	assert.Contains(t, result, "end_group: 10")
}
