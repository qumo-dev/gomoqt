package message

import (
	"bytes"
	"io"
	"testing"
)

type noByteReader struct {
	r io.Reader
}

func (r *noByteReader) Read(p []byte) (n int, err error) {
	return r.r.Read(p)
}

func BenchmarkReadMessageLengthByteReader(b *testing.B) {
	data := []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	r := bytes.NewReader(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(data)
		_, _ = ReadMessageLength(r)
	}
}

func BenchmarkReadMessageLengthNoByteReader(b *testing.B) {
	data := []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	r := bytes.NewReader(data)
	nr := &noByteReader{r}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(data)
		_, _ = ReadMessageLength(nr)
	}
}
