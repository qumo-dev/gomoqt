package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo(t *testing.T) {
	tests := map[string]PublishInfo{
		"default values": {},
		// "high priority": {
		// },
		// "low priority": {
		// },
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

// func TestInfoComparison(t *testing.T) {
// 	info1 := Info{}

// 	info2 := Info{}

// 	info3 := Info{}

// 	assert.Equal(t, info1, info2, "identical Info structs should be equal")
// 	assert.NotEqual(t, info1, info3, "different Info structs should not be equal")
// }
