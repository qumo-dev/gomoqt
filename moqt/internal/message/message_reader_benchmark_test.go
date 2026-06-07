package message

import (
	"bytes"
	"io"
	"testing"
)

type byteReaderOnly struct {
	r io.Reader
}

func (br *byteReaderOnly) Read(p []byte) (int, error) {
	return br.r.Read(p)
}

func BenchmarkReadMessageLength_Reader(b *testing.B) {
	data := []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	b.ReportAllocs()
	r := bytes.NewReader(data)
	for i := 0; i < b.N; i++ {
		r.Reset(data)
		_, _ = ReadMessageLength(&byteReaderOnly{r})
	}
}

func BenchmarkReadMessageLength_ByteReader(b *testing.B) {
	data := []byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	b.ReportAllocs()
	r := bytes.NewReader(data)
	for i := 0; i < b.N; i++ {
		r.Reset(data)
		_, _ = ReadMessageLength(r)
	}
}
