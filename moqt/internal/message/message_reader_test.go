package message

import (
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
)

func TestReadVarint(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected uint64
		n        int
		wantErr  bool
	}{
		"1 byte - zero": {
			input:    []byte{0x00},
			expected: 0,
			n:        1,
			wantErr:  false,
		},
		"1 byte - max": {
			input:    []byte{0x3f},
			expected: 63,
			n:        1,
			wantErr:  false,
		},
		"2 bytes - min": {
			input:    []byte{0x40, 0x40},
			expected: 64,
			n:        2,
			wantErr:  false,
		},
		"2 bytes - max": {
			input:    []byte{0x7f, 0xff},
			expected: 16383,
			n:        2,
			wantErr:  false,
		},
		"4 bytes - min": {
			input:    []byte{0x80, 0x00, 0x40, 0x00},
			expected: 16384,
			n:        4,
			wantErr:  false,
		},
		"8 bytes - min": {
			input:    []byte{0xc0, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00},
			expected: 1073741824,
			n:        8,
			wantErr:  false,
		},
		"empty buffer": {
			input:   []byte{},
			wantErr: true,
		},
		"incomplete 2 bytes": {
			input:   []byte{0x40},
			wantErr: true,
		},
		"incomplete 4 bytes": {
			input:   []byte{0x80, 0x00, 0x00},
			wantErr: true,
		},
		"incomplete 8 bytes": {
			input:   []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, n, err := ReadVarint(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
				assert.Equal(t, tt.n, n)
			}
		})
	}
}

// TestReadMessageLength runs every case through the io.ByteReader fast path
// (bytes.Reader) and through readers that hide ReadByte and so take the
// fallback path, delivering one byte per Read or the last bytes together with
// io.EOF. Both paths must decode identically and report a stream that ends
// before the varint as io.EOF and one that ends inside it as
// io.ErrUnexpectedEOF.
func TestReadMessageLength(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected uint64
		wantErr  error
	}{
		"zero":              {input: []byte{0x00}, expected: 0},
		"1-byte max":        {input: []byte{0x3f}, expected: 63},
		"2-byte varint":     {input: []byte{0x40, 0x80}, expected: 128},
		"2-byte varint 256": {input: []byte{0x41, 0x00}, expected: 256},
		"2-byte max":        {input: []byte{0x7f, 0xff}, expected: 16383},
		"4-byte varint":     {input: []byte{0x80, 0x01, 0x00, 0x00}, expected: 65536},
		"8-byte varint":     {input: []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}, expected: 65536},
		"empty input":       {input: []byte{}, wantErr: io.EOF},
		"incomplete 2-byte": {input: []byte{0x40}, wantErr: io.ErrUnexpectedEOF},
		"incomplete 4-byte": {input: []byte{0x80, 0x01}, wantErr: io.ErrUnexpectedEOF},
	}
	readers := map[string]func([]byte) io.Reader{
		"byte reader":       func(b []byte) io.Reader { return bytes.NewReader(b) },
		"one byte per read": func(b []byte) io.Reader { return iotest.OneByteReader(bytes.NewReader(b)) },
		"data with io.EOF":  func(b []byte) io.Reader { return iotest.DataErrReader(bytes.NewReader(b)) },
	}

	for name, tt := range tests {
		for readerName, newReader := range readers {
			t.Run(name+"/"+readerName, func(t *testing.T) {
				result, err := ReadMessageLength(newReader(tt.input))
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
					return
				}
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			})
		}
	}
}

// TestReadMessageLength_ReadError ensures a transport error is passed on as is,
// not mistaken for the end of the stream.
func TestReadMessageLength_ReadError(t *testing.T) {
	_, err := ReadMessageLength(iotest.ErrReader(iotest.ErrTimeout))
	assert.ErrorIs(t, err, iotest.ErrTimeout)
}

func TestReadBytes(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected []byte
		n        int
		wantErr  bool
	}{
		"empty bytes": {
			input:    []byte{0x00},
			expected: []byte{},
			n:        1,
			wantErr:  false,
		},
		"single byte": {
			input:    []byte{0x01, 0x42},
			expected: []byte{0x42},
			n:        2,
			wantErr:  false,
		},
		"multiple bytes": {
			input:    []byte{0x03, 0x41, 0x42, 0x43},
			expected: []byte{0x41, 0x42, 0x43},
			n:        4,
			wantErr:  false,
		},
		"incomplete data": {
			input:   []byte{0x05, 0x41, 0x42},
			wantErr: true,
		},
		"invalid varint": {
			input:   []byte{},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, n, err := ReadBytes(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
				assert.Equal(t, tt.n, n)
			}
		})
	}
}

func TestReadString(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected string
		n        int
		wantErr  bool
	}{
		"empty string": {
			input:    []byte{0x00},
			expected: "",
			n:        1,
			wantErr:  false,
		},
		"simple string": {
			input:    []byte{0x05, 0x68, 0x65, 0x6c, 0x6c, 0x6f}, // "hello"
			expected: "hello",
			n:        6,
			wantErr:  false,
		},
		"incomplete string": {
			input:   []byte{0x05, 0x68, 0x65},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, n, err := ReadString(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
				assert.Equal(t, tt.n, n)
			}
		})
	}
}

func TestReadStringArray(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected []string
		n        int
		wantErr  bool
	}{
		"empty array": {
			input:    []byte{0x00},
			expected: []string{},
			n:        1,
			wantErr:  false,
		},
		"single element": {
			input:    []byte{0x01, 0x05, 0x68, 0x65, 0x6c, 0x6c, 0x6f}, // ["hello"]
			expected: []string{"hello"},
			n:        7,
			wantErr:  false,
		},
		"multiple elements": {
			input:    []byte{0x02, 0x05, 0x68, 0x65, 0x6c, 0x6c, 0x6f, 0x05, 0x77, 0x6f, 0x72, 0x6c, 0x64}, // ["hello", "world"]
			expected: []string{"hello", "world"},
			n:        13,
			wantErr:  false,
		},
		"incomplete array": {
			input:   []byte{0x01, 0x05, 0x68, 0x65},
			wantErr: true,
		},
		"invalid count": {
			input:   []byte{},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			result, n, err := ReadStringArray(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
				assert.Equal(t, tt.n, n)
			}
		})
	}
}
