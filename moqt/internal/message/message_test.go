package message

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVarintLen(t *testing.T) {
	tests := map[string]struct {
		input    uint64
		expected int
	}{
		"zero":            {0, 1},
		"max varint1":     {maxVarInt1, 1},
		"max varint1 + 1": {maxVarInt1 + 1, 2},
		"max varint2":     {maxVarInt2, 2},
		"max varint2 + 1": {maxVarInt2 + 1, 4},
		"max varint4":     {maxVarInt4, 4},
		"max varint4 + 1": {maxVarInt4 + 1, 8},
		"max varint8":     {maxVarInt8, 8},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := VarintLen(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVarintLenPanic(t *testing.T) {
	assert.Panics(t, func() {
		VarintLen(maxVarInt8 + 1)
	})
}

func TestStringLen(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected int
	}{
		"empty string":     {"", VarintLen(0) + 0},
		"short string":     {"hello", VarintLen(5) + 5},
		"longer string":    {string(make([]byte, 300)), VarintLen(300) + 300},
		"very long string": {string(make([]byte, 70000)), VarintLen(70000) + 70000},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := StringLen(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBytesLen(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected int
	}{
		"empty bytes":     {[]byte{}, VarintLen(0) + 0},
		"short bytes":     {[]byte("hello"), VarintLen(5) + 5},
		"longer bytes":    {make([]byte, 300), VarintLen(300) + 300},
		"very long bytes": {make([]byte, 70000), VarintLen(70000) + 70000},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := BytesLen(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringArrayLen(t *testing.T) {
	tests := map[string]struct {
		input    []string
		expected int
	}{
		"empty array":       {[]string{}, VarintLen(0)},
		"single element":    {[]string{"hello"}, VarintLen(1) + StringLen("hello")},
		"multiple elements": {[]string{"hello", "world", ""}, VarintLen(3) + StringLen("hello") + StringLen("world") + StringLen("")},
		"long strings":      {[]string{string(make([]byte, 100)), string(make([]byte, 200))}, VarintLen(2) + StringLen(string(make([]byte, 100))) + StringLen(string(make([]byte, 200)))},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result := StringArrayLen(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
