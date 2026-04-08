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
