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
// It optimizes for io.ByteReader to avoid heap allocations caused by
// interface-induced escape analysis when passing slices to io.ReadFull.
func ReadMessageLength(r io.Reader) (uint64, error) {
	br, ok := r.(io.ByteReader)
	if !ok {
		// Fallback for non-ByteReader: use a small array that will escape
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:1]); err != nil {
			return 0, err
		}

		b0 := buf[0]
		l := 1 << ((b0 & 0xc0) >> 6)
		if l == 1 {
			return uint64(b0 & 0x3f), nil
		}

		if _, err := io.ReadFull(r, buf[1:l]); err != nil {
			return 0, err
		}

		val, _, err := ReadVarint(buf[:l])
		return val, err
	}

	// Fast path for io.ByteReader
	b0, err := br.ReadByte()
	if err != nil {
		return 0, err
	}

	l := 1 << ((b0 & 0xc0) >> 6)
	if l == 1 {
		return uint64(b0 & 0x3f), nil
	}

	var b [8]byte
	b[0] = b0
	for i := 1; i < l; i++ {
		c, err := br.ReadByte()
		if err != nil {
			// Explicitly map EOF to ErrUnexpectedEOF for partial varints to preserve semantics
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		b[i] = c
	}

	val, _, err := ReadVarint(b[:l])
	return val, err
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
