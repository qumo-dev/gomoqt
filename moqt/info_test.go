package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo(t *testing.T) {
	tests := map[string]PublishInfo{
		"default values": {},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			info := PublishInfo{}

			assert.Equal(t, tt, info)
		})
	}
}

func TestInfoZeroValue(t *testing.T) {
	var info PublishInfo

	assert.Equal(t, PublishInfo{}, info)
}

func TestPublishInfo_String(t *testing.T) {
	info := PublishInfo{
		Priority:   5,
		Ordered:    true,
		MaxLatency: 100,
		StartGroup: 1,
		EndGroup:   10,
	}

	result := info.String()
	assert.Contains(t, result, "priority: 5")
	assert.Contains(t, result, "ordered: true")
	assert.Contains(t, result, "max_latency_ms: 100")
	assert.Contains(t, result, "start_group: 1")
	assert.Contains(t, result, "end_group: 10")
}

func TestResolveTrackInfo(t *testing.T) {
	tests := map[string]struct {
		config SubscribeConfig
		info   PublishInfo
		expect SubscribeConfig
	}{
		"publisher values win when higher": {
			config: SubscribeConfig{
				Priority:   1,
				Ordered:    false,
				MaxLatency: 50,
				StartGroup: 0,
				EndGroup:   5,
			},
			info: PublishInfo{
				Priority:   10,
				Ordered:    true,
				MaxLatency: 200,
				StartGroup: 3,
				EndGroup:   20,
			},
			expect: SubscribeConfig{
				Priority:   10,
				Ordered:    true,
				MaxLatency: 200,
				StartGroup: 3,
				EndGroup:   20,
			},
		},
		"subscriber values win when higher": {
			config: SubscribeConfig{
				Priority:   20,
				Ordered:    true,
				MaxLatency: 500,
				StartGroup: 10,
				EndGroup:   30,
			},
			info: PublishInfo{
				Priority:   5,
				Ordered:    false,
				MaxLatency: 100,
				StartGroup: 1,
				EndGroup:   10,
			},
			expect: SubscribeConfig{
				Priority:   20,
				Ordered:    true,
				MaxLatency: 500,
				StartGroup: 10,
				EndGroup:   30,
			},
		},
		"zero values": {
			config: SubscribeConfig{},
			info:   PublishInfo{},
			expect: SubscribeConfig{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := ResolveTrackInfo(tt.config, tt.info)
			assert.Equal(t, tt.expect, result)
		})
	}
}
