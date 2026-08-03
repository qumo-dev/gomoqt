package message

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadBytes_TooLarge(t *testing.T) {
	var buf [8]byte
	val := uint64(MaxMessageSize) + 1

	buf[0] = byte((val >> 56) | 0xc0)
	buf[1] = byte(val >> 48)
	buf[2] = byte(val >> 40)
	buf[3] = byte(val >> 32)
	buf[4] = byte(val >> 24)
	buf[5] = byte(val >> 16)
	buf[6] = byte(val >> 8)
	buf[7] = byte(val)

	_, _, err := ReadBytes(buf[:])
	assert.Error(t, err)
	if err != nil {
		assert.Equal(t, ErrMessageTooLarge, err)
	}
}

func TestReadStringArray_TooLarge(t *testing.T) {
	var buf [8]byte
	val := uint64(MaxMessageSize) + 1

	buf[0] = byte((val >> 56) | 0xc0)
	buf[1] = byte(val >> 48)
	buf[2] = byte(val >> 40)
	buf[3] = byte(val >> 32)
	buf[4] = byte(val >> 24)
	buf[5] = byte(val >> 16)
	buf[6] = byte(val >> 8)
	buf[7] = byte(val)

	_, _, err := ReadStringArray(buf[:])
	assert.Error(t, err)
	if err != nil {
		assert.Equal(t, ErrMessageTooLarge, err)
	}
}
