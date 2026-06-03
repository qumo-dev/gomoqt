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

// ReadMessageLength reads a QUIC varint from an io.Reader
func ReadMessageLength(r io.Reader) (uint64, error) {
	var firstByte byte
	var err error

	if br, ok := r.(io.ByteReader); ok {
		firstByte, err = br.ReadByte()
	} else {
		var buf [1]byte
		bufSlice := buf[:]
		_, err = io.ReadFull(r, bufSlice)
		firstByte = bufSlice[0]
	}

	if err != nil {
		return 0, err
	}

	// Determine the length from the first two bits
	l := 1 << ((firstByte & 0xc0) >> 6)

	if l == 1 {
		return uint64(firstByte & 0x3f), nil
	}

	if br, ok := r.(io.ByteReader); ok {
		var val uint64 = uint64(firstByte & 0x3f)
		for i := 1; i < l; i++ {
			b, err := br.ReadByte()
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
			val = (val << 8) | uint64(b)
		}
		return val, nil
	}

	var buf [8]byte
	bufSlice := buf[:l-1]
	_, err = io.ReadFull(r, bufSlice)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, err
	}

	var val uint64 = uint64(firstByte & 0x3f)
	for i := 0; i < l-1; i++ {
		val = (val << 8) | uint64(bufSlice[i])
	}
	return val, nil
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
