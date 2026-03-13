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
			config := TrackConfig{
				TrackPriority: tt.trackPriority,
			}

			assert.Equal(t, tt.trackPriority, config.TrackPriority)
		})
	}
}

func TestTrackConfig_String(t *testing.T) {
	tests := map[string]struct {
		config   TrackConfig
		expected string
	}{
		"default values": {
			config: TrackConfig{
				TrackPriority: TrackPriority(0),
			},
			expected: "{ track_priority: 0, ordered: false, max_latency_ms: 0, start_group: 0, end_group: 0 }",
		},
		"specific values": {
			config: TrackConfig{
				TrackPriority: TrackPriority(128),
			},
			expected: "{ track_priority: 128, ordered: false, max_latency_ms: 0, start_group: 0, end_group: 0 }",
		},
		"high values": {
			config: TrackConfig{
				TrackPriority: TrackPriority(255),
			},
			expected: "{ track_priority: 255, ordered: false, max_latency_ms: 0, start_group: 0, end_group: 0 }",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := tt.config.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrackConfig_ZeroValue(t *testing.T) {
	var config TrackConfig

	assert.Equal(t, TrackPriority(0), config.TrackPriority)
}

func TestTrackConfig_Comparison(t *testing.T) {
	config1 := TrackConfig{
		TrackPriority: TrackPriority(128),
	}

	config2 := TrackConfig{
		TrackPriority: TrackPriority(128),
	}

	config3 := TrackConfig{
		TrackPriority: TrackPriority(64),
	}

	assert.Equal(t, config1, config2)
	assert.NotEqual(t, config1, config3)
}
