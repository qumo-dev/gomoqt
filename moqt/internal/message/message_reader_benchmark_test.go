package message

import (
	"bytes"
	"testing"
)

func BenchmarkReadMessageLength(b *testing.B) {
	buf := []byte{0x40, 100}

	b.Run("io.Reader", func(b *testing.B) {
		r := bytes.NewReader(buf)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r.Reset(buf)
			ReadMessageLength(r)
		}
	})

}
