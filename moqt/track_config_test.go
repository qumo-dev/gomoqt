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
				SubscriberPriority: tt.trackPriority,
			}

			assert.Equal(t, tt.trackPriority, config.SubscriberPriority)
		})
	}
}

func TestTrackConfig_ZeroValue(t *testing.T) {
	var config TrackConfig

	assert.Equal(t, TrackPriority(0), config.SubscriberPriority)
}

func TestTrackConfig_Comparison(t *testing.T) {
	config1 := TrackConfig{
		SubscriberPriority: TrackPriority(128),
	}

	config2 := TrackConfig{
		SubscriberPriority: TrackPriority(128),
	}

	config3 := TrackConfig{
		SubscriberPriority: TrackPriority(64),
	}

	assert.Equal(t, config1, config2)
	assert.NotEqual(t, config1, config3)
}
