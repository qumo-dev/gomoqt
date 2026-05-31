package message

import (
	"io"
	"testing"
)

type mockReader struct {
	data []byte
	pos  int
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n = copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func TestReadMessageLengthNoByteReader(t *testing.T) {
	cases := []struct {
		name     string
		data     []byte
		expected uint64
		wantErr  bool
	}{
		{"1-byte", []byte{0x01}, 1, false},
		{"2-byte", []byte{0x40, 100}, 100, false},
		{"4-byte", []byte{0x80, 0x00, 0x01, 0x00}, 256, false},
		{"8-byte", []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00}, 256, false},
		{"incomplete 1-byte", []byte{}, 0, true},
		{"incomplete 2-byte", []byte{0x40}, 0, true},
		{"incomplete 4-byte", []byte{0x80, 0x00, 0x01}, 0, true},
		{"incomplete 8-byte", []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}, 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &mockReader{data: c.data}
			val, err := ReadMessageLength(r)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if val != c.expected {
					t.Errorf("expected %d, got %d", c.expected, val)
				}
			}
		})
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestReadMessageLengthError(t *testing.T) {
	_, err := ReadMessageLength(errReader{})
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

type mockByteReaderError struct {
	data []byte
	pos  int
}

func (m *mockByteReaderError) ReadByte() (byte, error) {
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	b := m.data[m.pos]
	m.pos++
	if m.pos == 2 {
		return 0, io.EOF
	}
	return b, nil
}

func (m *mockByteReaderError) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func TestReadMessageLengthByteReaderError(t *testing.T) {
	r := &mockByteReaderError{data: []byte{0x40, 100}}
	_, err := ReadMessageLength(r)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

type mockByteReaderFirstError struct{}

func (m *mockByteReaderFirstError) ReadByte() (byte, error) {
	return 0, io.EOF
}

func (m *mockByteReaderFirstError) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func TestReadMessageLengthByteReaderFirstError(t *testing.T) {
	r := &mockByteReaderFirstError{}
	_, err := ReadMessageLength(r)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}
