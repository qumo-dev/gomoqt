package message

import (
	"bytes"
	"io"
	"testing"

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
		t.Run(name, func(t *testing.T) {
			r := bytes.NewReader(tt.input)
			result, err := ReadMessageLength(r)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
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

func BenchmarkReadMessageLength(b *testing.B) {
	input := []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}

	r := bytes.NewReader(input)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(input)
		_, _ = ReadMessageLength(r)
	}
}

type errReader struct{
	err error
}
func (e *errReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}

type errByteReader struct{
	err error
}
func (e *errByteReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}
func (e *errByteReader) ReadByte() (byte, error) {
	return 0, e.err
}

type noByteReader struct{
	r io.Reader
}
func (n *noByteReader) Read(p []byte) (int, error) {
	return n.r.Read(p)
}

func TestReadMessageLengthErrors(t *testing.T) {
	t.Run("ByteReader first byte error", func(t *testing.T) {
		_, err := ReadMessageLength(&errByteReader{err: assert.AnError})
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("Reader first byte error", func(t *testing.T) {
		_, err := ReadMessageLength(&errReader{err: assert.AnError})
		assert.ErrorIs(t, err, assert.AnError)
	})

	// Add more error coverage for l=4 short read to cover specific branches in switch
	t.Run("ByteReader 4-byte read error on 2nd", func(t *testing.T) {
		r := &partialReader{data: []byte{0x80, 0x00}}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("ByteReader 4-byte read error on 3rd", func(t *testing.T) {
		r := &partialReader{data: []byte{0x80, 0x00, 0x00}}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, assert.AnError)
	})

	// Test 8-byte loop errors
	t.Run("ByteReader 8-byte error on 5th byte", func(t *testing.T) {
		r := &partialReader{data: []byte{0xc0, 0x00, 0x00, 0x00, 0x00}}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("ByteReader 8-byte error on 2nd byte", func(t *testing.T) {
		r := &partialReader{data: []byte{0xc0, 0x00}}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("ByteReader 8-byte error on 3rd byte", func(t *testing.T) {
		r := &partialReader{data: []byte{0xc0, 0x00, 0x00}}
		_, err := ReadMessageLength(r)
		assert.ErrorIs(t, err, assert.AnError)
	})

	// Test remaining coverage paths for short reads
	paths := []struct{
		name string
		input []byte
		isByteReader bool
	}{
		{"ByteReader 2-byte short", []byte{0x40}, true},
		{"ByteReader 4-byte short", []byte{0x80, 0x00, 0x00}, true},
		{"ByteReader 8-byte short", []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, true},
		{"Reader 2-byte short", []byte{0x40}, false},
		{"Reader 4-byte short", []byte{0x80, 0x00, 0x00}, false},
		{"Reader 8-byte short", []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, false},
	}

	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			var r io.Reader = bytes.NewReader(p.input)
			if !p.isByteReader {
				r = &noByteReader{r: r}
			}
			_, err := ReadMessageLength(r)
			assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
		})
	}
}

// A test case that simulates reading an exact buffer and getting an unexpected EOF
type partialReader struct {
	data []byte
	n    int
}
func (p *partialReader) Read(b []byte) (n int, err error) {
	if p.n >= len(p.data) {
		return 0, assert.AnError
	}
	n = copy(b, p.data[p.n:])
	p.n += n
	if n < len(b) {
		return n, assert.AnError
	}
	return n, nil
}
func (p *partialReader) ReadByte() (byte, error) {
	if p.n >= len(p.data) {
		return 0, assert.AnError
	}
	b := p.data[p.n]
	p.n++
	return b, nil
}

func TestReadMessageLengthOtherErrors(t *testing.T) {
	// byte reader with custom error
	r1 := &partialReader{data: []byte{0x40}}
	_, err := ReadMessageLength(r1)
	assert.ErrorIs(t, err, assert.AnError)

	r2 := &partialReader{data: []byte{0x80, 0x00, 0x00}}
	_, err = ReadMessageLength(r2)
	assert.ErrorIs(t, err, assert.AnError)

	r3 := &partialReader{data: []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}}
	_, err = ReadMessageLength(r3)
	assert.ErrorIs(t, err, assert.AnError)

	// non-byte reader with custom error
	nr1 := &noByteReader{r: &partialReader{data: []byte{0x40}}}
	_, err = ReadMessageLength(nr1)
	assert.ErrorIs(t, err, assert.AnError)

	nr2 := &noByteReader{r: &partialReader{data: []byte{0x80, 0x00, 0x00}}}
	_, err = ReadMessageLength(nr2)
	assert.ErrorIs(t, err, assert.AnError)

	nr3 := &noByteReader{r: &partialReader{data: []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}}}
	_, err = ReadMessageLength(nr3)
	assert.ErrorIs(t, err, assert.AnError)
}

// To hit the "return 0, io.ErrUnexpectedEOF" at the end of the function (for invalid length which shouldn't happen but the compiler requires a return or exhaustive switch). Actually wait, l is derived from 1 << ((b0 & 0xc0) >> 6), so l is always 1, 2, 4, or 8. Since 1 is handled earlier, it's 2, 4, 8. Both switch statements cover 2, 4, 8. Thus the end is technically unreachable, but to cover the default case in switch if it existed we'd need a different length. We cannot trigger the end unless `l` is not 2, 4, or 8. Since `l` is mathematically guaranteed to be 1, 2, 4, 8, the end `return 0, io.ErrUnexpectedEOF` is UNREACHABLE.
// Oh wait, `b1, err = br.ReadByte()` for l=4 has:
// 			if err == nil {
//				b2, err = br.ReadByte()
//			}
//			if err == nil {
//				b3, err = br.ReadByte()
//			}
// If the second byte succeeds but third fails with non-EOF error... Wait, if `err != nil`, it maps EOF and returns. If it succeeds, it returns.
// What about the switch not having a default?

func TestReadMessageLengthUnreachable(t *testing.T) {
    // Cannot reach it normally. Let's see if there are other uncovered lines.
}
