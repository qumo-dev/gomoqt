package message

import (
	"fmt"
)

func WriteVarint(b []byte, i uint64) ([]byte, int, error) {
	if i <= maxVarInt1 {
		b = append(b, byte(i))
		return b, 1, nil
	}
	if i <= maxVarInt2 {
		b = append(b,
			uint8(i>>8)|0x40,
			byte(i),
		)
		return b, 2, nil
	}
	if i <= maxVarInt4 {
		b = append(b,
			uint8(i>>24)|0x80,
			uint8(i>>16),
			uint8(i>>8),
			byte(i),
		)
		return b, 4, nil
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
		return b, 8, nil
	}
	return nil, 0, fmt.Errorf("%#x doesn't fit into 62 bits", i)
}

func WriteBytes(dest []byte, b []byte) ([]byte, int, error) {
	dest, n, err := WriteVarint(dest, uint64(len(b)))
	if err != nil {
		return nil, 0, err
	}
	dest = append(dest, b...)
	return dest, n + len(b), nil
}

func WriteString(dest []byte, s string) ([]byte, int, error) {
	return WriteBytes(dest, []byte(s))
}

func WriteStringArray(dest []byte, arr []string) ([]byte, int, error) {
	dest, n, err := WriteVarint(dest, uint64(len(arr)))
	if err != nil {
		return nil, 0, err
	}
	var m int
	for _, str := range arr {
		dest, m, err = WriteString(dest, str)
		if err != nil {
			return nil, 0, err
		}
		n += m
	}
	return dest, n, nil
}

const (
	maxVarInt1 = 1<<(8-2) - 1
	maxVarInt2 = 1<<(16-2) - 1
	maxVarInt4 = 1<<(32-2) - 1
	maxVarInt8 = 1<<(64-2) - 1
)

func WriteMessageLength(b []byte, size uint64) ([]byte, int, error) {
	return WriteVarint(b, size)
}
