package message

import (
	"fmt"
)

func WriteVarint(b []byte, i uint64) ([]byte, int) {
	if i <= maxVarInt1 {
		b = append(b, byte(i))
		return b, 1
	}
	if i <= maxVarInt2 {
		b = append(b,
			uint8(i>>8)|0x40,
			byte(i),
		)
		return b, 2
	}
	if i <= maxVarInt4 {
		b = append(b,
			uint8(i>>24)|0x80,
			uint8(i>>16),
			uint8(i>>8),
			byte(i),
		)
		return b, 4
	}
	if i <= maxVarInt8 {
		b = append(b,
			uint8(i>>56)|0xc0,
			uint8(i>>48),
			uint8(i>>40),
			uint8(i>>32),
			uint8(i>>24),
			uint8(i>>16),
			uint8(i>>8),
			byte(i),
		)
		return b, 8
	}
	panic(fmt.Sprintf("%#x doesn't fit into 62 bits", i))
}

func WriteBytes(dest []byte, b []byte) ([]byte, int) {
	dest, n := WriteVarint(dest, uint64(len(b)))
	dest = append(dest, b...)
	return dest, n + len(b)
}

func WriteString(dest []byte, s string) ([]byte, int) {
	dest, n := WriteVarint(dest, uint64(len(s)))
	dest = append(dest, s...)
	return dest, n + len(s)
}

func WriteStringArray(dest []byte, arr []string) ([]byte, int) {
	dest, n := WriteVarint(dest, uint64(len(arr)))
	var m int
	for _, str := range arr {
		dest, m = WriteString(dest, str)
		n += m
	}
	return dest, n
}

func WriteParameters(dest []byte, params map[uint64][]byte) ([]byte, int) {
	dest, n := WriteVarint(dest, uint64(len(params)))
	var m int
	for key, value := range params {
		dest, m = WriteVarint(dest, uint64(key))
		n += m
		dest, m = WriteBytes(dest, value)
		n += m
	}
	return dest, n
}

const (
	maxVarInt1 = 1<<(8-2) - 1
	maxVarInt2 = 1<<(16-2) - 1
	maxVarInt4 = 1<<(32-2) - 1
	maxVarInt8 = 1<<(64-2) - 1
)

func WriteMessageLength(b []byte, size uint64) ([]byte, int) {
	return WriteVarint(b, size)
}
