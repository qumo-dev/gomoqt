package message

import (
	"io"
	"math"
)

func ReadVarint(b []byte) (uint64, int, error) {
	if len(b) < 1 {
		return 0, 0, io.EOF
	}
	l := 1 << ((b[0] & 0xc0) >> 6)
	if len(b) < l {
		return 0, 0, io.EOF
	}
	var i uint64
	switch l {
	case 1:
		i = uint64(b[0] & (0xff - 0xc0))
	case 2:
		i = uint64(b[0]&0x3f)<<8 | uint64(b[1])
	case 4:
		i = uint64(b[0]&0x3f)<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
	case 8:
		i = uint64(b[0]&0x3f)<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
			uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
	}
	return i, l, nil
}

// ReadMessageLength reads a QUIC varint from an io.Reader.
// It uses a stack-allocated buffer and a fast path for io.ByteReader
// to avoid heap allocations and improve decoding performance.
func ReadMessageLength(r io.Reader) (uint64, error) {
	var buf [8]byte

	// Fast path: use ReadByte if available to bypass io.ReadFull overhead
	if br, ok := r.(io.ByteReader); ok {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		buf[0] = b
	} else {
		_, err := io.ReadFull(r, buf[:1])
		if err != nil {
			return 0, err
		}
	}

	// Determine the length from the first two bits
	l := 1 << ((buf[0] & 0xc0) >> 6)

	// Read remaining bytes if needed
	if l > 1 {
		_, err := io.ReadFull(r, buf[1:l])
		if err != nil {
			return 0, err
		}
	}

	// Parse the varint inline to prevent buf escaping to the heap
	var i uint64
	switch l {
	case 1:
		i = uint64(buf[0] & (0xff - 0xc0))
	case 2:
		i = uint64(buf[0]&0x3f)<<8 | uint64(buf[1])
	case 4:
		i = uint64(buf[0]&0x3f)<<24 | uint64(buf[1])<<16 | uint64(buf[2])<<8 | uint64(buf[3])
	case 8:
		i = uint64(buf[0]&0x3f)<<56 | uint64(buf[1])<<48 | uint64(buf[2])<<40 | uint64(buf[3])<<32 |
			uint64(buf[4])<<24 | uint64(buf[5])<<16 | uint64(buf[6])<<8 | uint64(buf[7])
	}
	return i, nil
}

func ReadBytes(b []byte) ([]byte, int, error) {
	num, n, err := ReadVarint(b)
	if err != nil {
		return nil, 0, err
	}
	b = b[n:]
	if num > math.MaxInt {
		panic("byte slice too large")
	}

	if uint64(len(b)) < num {
		return b, n + len(b), io.EOF
	}

	return b[:num], n + int(num), nil
}

func ReadString(b []byte) (string, int, error) {
	str, n, err := ReadBytes(b)
	if err != nil {
		return "", 0, err
	}
	return string(str), n, nil
}

func ReadStringArray(b []byte) ([]string, int, error) {
	count, total, err := ReadVarint(b)
	if err != nil {
		return nil, 0, err
	}

	if count > math.MaxInt {
		panic("string array too large")
	}

	b = b[total:]

	arr := make([]string, 0, count)
	for range count {
		str, n, err := ReadString(b)
		if err != nil {
			return nil, 0, err
		}
		arr = append(arr, str)
		b = b[n:]
		total += n
	}

	return arr, total, nil
}
