package message

import (
	"io"
	"testing"
)

type dummyReader struct {
	buf []byte
	pos int
}

func (r *dummyReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.buf) {
		return 0, io.EOF
	}
	n = copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}

func TestReadMessageLength_Fallback(t *testing.T) {
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
			r := &dummyReader{buf: tt.input, pos: 0}
			result, err := ReadMessageLength(r)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Fatalf("expected %d, got %d", tt.expected, result)
				}
			}
		})
	}
}
