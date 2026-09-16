package message_test

import (
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestZigzagEncode(t *testing.T) {
	tests := map[string]struct {
		input    int64
		expected uint64
	}{
		"zero":           {input: 0, expected: 0},
		"minus one":      {input: -1, expected: 1},
		"one":            {input: 1, expected: 2},
		"minus two":      {input: -2, expected: 3},
		"two":            {input: 2, expected: 4},
		"large positive": {input: 1 << 60, expected: 1 << 61},
		"large negative": {input: -(1 << 60), expected: 1<<61 - 1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, message.ZigzagEncode(tt.input))
		})
	}
}

func TestZigzagDecode(t *testing.T) {
	tests := map[string]struct {
		input    uint64
		expected int64
	}{
		"zero":  {input: 0, expected: 0},
		"one":   {input: 1, expected: -1},
		"two":   {input: 2, expected: 1},
		"three": {input: 3, expected: -2},
		"four":  {input: 4, expected: 2},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, message.ZigzagDecode(tt.input))
		})
	}
}

func TestZigzagEncode_RoundTrip(t *testing.T) {
	values := []int64{0, 1, -1, 42, -42, 1<<50 - 1, -(1 << 50)}
	for _, v := range values {
		assert.Equal(t, v, message.ZigzagDecode(message.ZigzagEncode(v)))
	}
}
