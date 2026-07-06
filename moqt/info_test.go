package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfoZeroValue(t *testing.T) {
	var info PublishInfo

	assert.Equal(t, PublishInfo{}, info)
}

func TestPublishInfo_String(t *testing.T) {
	info := PublishInfo{
		Priority:   5,
		Ordered:    true,
		MaxLatency: 100,
		Timescale:  90000,
	}

	result := info.String()
	assert.Contains(t, result, "priority: 5")
	assert.Contains(t, result, "ordered: true")
	assert.Contains(t, result, "max_latency_ms: 100")
	assert.Contains(t, result, "timescale: 90000")
}

func TestPublishInfo_TimescaleOrDefault(t *testing.T) {
	tests := map[string]struct {
		info     PublishInfo
		expected uint64
	}{
		"zero value defaults to 1000": {info: PublishInfo{}, expected: DefaultTimescale},
		"explicit timescale is kept":  {info: PublishInfo{Timescale: 48000}, expected: 48000},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.info.timescaleOrDefault())
		})
	}
}
