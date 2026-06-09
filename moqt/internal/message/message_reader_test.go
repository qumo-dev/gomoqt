package message

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockReader struct {
	err       error
	errAt     int
	readCount int
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	m.readCount++
	if m.errAt == 0 || m.readCount >= m.errAt {
		return 0, m.err
	}
	p[0] = 0xc0
	return 1, nil
}

// byteReader is a wrapper to force testing the io.ByteReader path
type byteReader struct {
	io.Reader
	err       error
	errAt     int
	readCount int
}

func (b *byteReader) ReadByte() (byte, error) {
	b.readCount++
	if b.errAt > 0 && b.readCount == b.errAt {
		return 0, b.err
	}
	var buf [1]byte
	n, err := b.Read(buf[:])
	if n == 1 {
		return buf[0], nil
	}
	if err == io.EOF {
		return 0, io.EOF
	}
	if err != nil {
		return 0, err
	}
	return 0, io.ErrUnexpectedEOF
}

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

func TestReadMessageLength(t *testing.T) {
	tests := map[string]struct {
		input    []byte
		expected uint64
		wantErr  bool
	}{
		"zero": {
			input:    []byte{0x00},
			expected: 0,
			wantErr:  false,
		},
		"small value": {
			input:    []byte{0x3f},
			expected: 63,
			wantErr:  false,
		},
		"2-byte varint": {
			input:    []byte{0x40, 0x80},
			expected: 128,
			wantErr:  false,
		},
		"2-byte varint 256": {
			input:    []byte{0x41, 0x00},
			expected: 256,
			wantErr:  false,
		},
		"2-byte max": {
			input:    []byte{0x7f, 0xff},
			expected: 16383,
			wantErr:  false,
		},
		"4-byte varint": {
			input:    []byte{0x80, 0x01, 0x00, 0x00},
			expected: 65536,
			wantErr:  false,
		},
		"8-byte varint": {
			input:    []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00},
			expected: 65536,
			wantErr:  false,
		},
		"empty input": {
			input:   []byte{},
			wantErr: true,
		},
		"incomplete 2-byte": {
			input:   []byte{0x40},
			wantErr: true,
		},
		"incomplete 4-byte": {
			input:   []byte{0x80, 0x01},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name+" without ByteReader", func(t *testing.T) {
			r := bytes.NewReader(tt.input)
			result, err := ReadMessageLength(r)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})

		t.Run(name+" with ByteReader", func(t *testing.T) {
			r := &byteReader{Reader: bytes.NewReader(tt.input)}
			result, err := ReadMessageLength(r)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}

	// Test specific incomplete read scenarios for ByteReader
	t.Run("ByteReader incomplete 2-byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0x40})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader incomplete 4-byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x01, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader incomplete 8-byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x01, 0x00, 0x00, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	// Additional tests to cover error paths in switch branches
	t.Run("ByteReader error mid-stream 4-byte 2nd byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x01})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader error mid-stream 4-byte 3rd byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x01, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader error mid-stream 8-byte 2nd byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader error mid-stream 8-byte 4th byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader error mid-stream 8-byte 6th byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("ByteReader error mid-stream 8-byte 7th byte", func(t *testing.T) {
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	// Test underlying non-EOF errors
	t.Run("ByteReader generic error 2-byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0x40, 0x00}), err: errTest, errAt: 2}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})

	t.Run("ByteReader generic error 4-byte 2nd byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x00, 0x00, 0x00}), err: errTest, errAt: 2}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 4-byte 3rd byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x00, 0x00, 0x00}), err: errTest, errAt: 3}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 4-byte 4th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0x80, 0x00, 0x00, 0x00}), err: errTest, errAt: 4}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})

	t.Run("ByteReader generic error 8-byte 2nd byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 2}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 3rd byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 3}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 4th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 4}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 5th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 5}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 6th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 6}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 7th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 7}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
	t.Run("ByteReader generic error 8-byte 8th byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}), err: errTest, errAt: 8}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})

	// Non-ByteReader fast path tests
	t.Run("Non-ByteReader generic error 1st byte", func(t *testing.T) {
		errTest := errors.New("test error")
		// Use a simple wrapper that calls Read directly and returns our error
		wrapper := struct{ io.Reader }{&mockReader{err: errTest}}
		_, err := ReadMessageLength(wrapper)
		assert.ErrorIs(t, err, errTest)
	})

	t.Run("Non-ByteReader generic error 2nd byte", func(t *testing.T) {
		errTest := errors.New("test error")
		// Use a simple wrapper that calls Read directly and returns our error after 1 byte
		wrapper := struct{ io.Reader }{&mockReader{err: errTest, errAt: 2}}
		_, err := ReadMessageLength(wrapper)
		assert.ErrorIs(t, err, errTest)
	})

	t.Run("ByteReader generic error 1st byte", func(t *testing.T) {
		errTest := errors.New("test error")
		r := &byteReader{Reader: bytes.NewReader([]byte{0x40}), err: errTest, errAt: 1}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, errTest)
	})
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
