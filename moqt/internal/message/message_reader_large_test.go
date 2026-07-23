package message

import (
	"github.com/stretchr/testify/assert"
	"math"
	"testing"
)

func TestReadBytes_TooLargeError(t *testing.T) {
	val := uint64(0x3fffffffffffffff)
	var b [8]byte
	b[0] = byte((val>>56)&0x3f | 0xc0)
	b[1] = byte(val >> 48)
	b[2] = byte(val >> 40)
	b[3] = byte(val >> 32)
	b[4] = byte(val >> 24)
	b[5] = byte(val >> 16)
	b[6] = byte(val >> 8)
	b[7] = byte(val)

	_, _, err := ReadBytes(b[:])

	if val > math.MaxInt {
		assert.EqualError(t, err, "byte slice too large")
	} else {
		assert.EqualError(t, err, "EOF")
	}
}

func TestReadStringArray_TooLargeError(t *testing.T) {
	val := uint64(0x3fffffffffffffff)
	var b [8]byte
	b[0] = byte((val>>56)&0x3f | 0xc0)
	b[1] = byte(val >> 48)
	b[2] = byte(val >> 40)
	b[3] = byte(val >> 32)
	b[4] = byte(val >> 24)
	b[5] = byte(val >> 16)
	b[6] = byte(val >> 8)
	b[7] = byte(val)

	_, _, err := ReadStringArray(b[:])

	if val > math.MaxInt {
		assert.EqualError(t, err, "string array too large")
	} else {
		assert.Error(t, err)
	}
}
