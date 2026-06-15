package message

import (
	"reflect"
	"testing"
)

func TestWriteVarint(t *testing.T) {
	tests := []struct {
		input    uint64
		expected []byte
		n        int
	}{
		{0, []byte{0}, 1},
		{1, []byte{1}, 1},
		{maxVarInt1, []byte{0x3f}, 1},
		{maxVarInt1 + 1, []byte{0x40, 0x40}, 2},
		{maxVarInt2, []byte{0x7f, 0xff}, 2},
		{maxVarInt2 + 1, []byte{0x80, 0x00, 0x40, 0x00}, 4},
		{maxVarInt4, []byte{0xbf, 0xff, 0xff, 0xff}, 4},
		{maxVarInt4 + 1, []byte{0xc0, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00}, 8},
		{maxVarInt8, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 8},
	}

	for _, tt := range tests {
		result, n := WriteVarint([]byte{}, tt.input)
		if n != tt.n {
			t.Errorf("WriteVarint(%d) n = %d, want %d", tt.input, n, tt.n)
		}
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("WriteVarint(%d) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestWriteVarintPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("WriteVarint should panic for large values")
		}
	}()
	WriteVarint([]byte{}, maxVarInt8+1)
}

func TestWriteBytes(t *testing.T) {
	tests := []struct {
		dest     []byte
		b        []byte
		expected []byte
		n        int
	}{
		{[]byte{}, []byte{}, []byte{0}, 1},
		{[]byte{}, []byte{1, 2, 3}, []byte{3, 1, 2, 3}, 4},
	}

	for _, tt := range tests {
		result, n := WriteBytes(tt.dest, tt.b)
		if n != tt.n {
			t.Errorf("WriteBytes n = %d, want %d", n, tt.n)
		}
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("WriteBytes = %v, want %v", result, tt.expected)
		}
	}
}

func TestWriteString(t *testing.T) {
	tests := []struct {
		dest     []byte
		s        string
		expected []byte
		n        int
	}{
		{[]byte{}, "", []byte{0}, 1},
		{[]byte{}, "abc", []byte{3, 'a', 'b', 'c'}, 4},
	}

	for _, tt := range tests {
		result, n := WriteString(tt.dest, tt.s)
		if n != tt.n {
			t.Errorf("WriteString n = %d, want %d", n, tt.n)
		}
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("WriteString = %v, want %v", result, tt.expected)
		}
	}
}

func TestWriteStringArray(t *testing.T) {
	tests := []struct {
		dest     []byte
		arr      []string
		expected []byte
		n        int
	}{
		{[]byte{}, []string{}, []byte{0}, 1},
		{[]byte{}, []string{"a", "bc"}, []byte{2, 1, 'a', 2, 'b', 'c'}, 6},
	}

	for _, tt := range tests {
		result, n := WriteStringArray(tt.dest, tt.arr)
		if n != tt.n {
			t.Errorf("WriteStringArray n = %d, want %d", n, tt.n)
		}
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("WriteStringArray = %v, want %v", result, tt.expected)
		}
	}
}
