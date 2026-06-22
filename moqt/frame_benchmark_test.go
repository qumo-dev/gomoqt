package moqt

import (
	"bytes"
	"io"
	"testing"
)

// BenchmarkFrame_Encode benchmarks frame encoding with different sizes
func BenchmarkFrame_Encode(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			frame := NewFrame(size)
			data := make([]byte, size)
			frame.Write(data)

			var buf bytes.Buffer
			buf.Grow(size + 8)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				buf.Reset()
				_ = frame.encode(&buf)
			}
		})
	}
}

// BenchmarkFrame_Decode benchmarks frame decoding with different sizes
func BenchmarkFrame_Decode(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			// Prepare encoded data
			frame := NewFrame(size)
			data := make([]byte, size)
			frame.Write(data)

			var buf bytes.Buffer
			_ = frame.encode(&buf)
			encodedData := buf.Bytes()

			reader := bytes.NewReader(encodedData)

			decodeFrame := NewFrame(size)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				reader.Reset(encodedData)
				decodeFrame.Reset()
				err := decodeFrame.decode(reader)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFrame_WriteAppend benchmarks appending data to frames
func BenchmarkFrame_WriteAppend(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			data := make([]byte, size)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame := NewFrame(size)
				_, _ = frame.Write(data)
			}
		})
	}
}

// BenchmarkFrame_Reuse benchmarks frame reuse with Reset
func BenchmarkFrame_Reuse(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			frame := NewFrame(size)
			data := make([]byte, size)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame.Reset()
				_, _ = frame.Write(data)
			}
		})
	}
}

// BenchmarkFrame_Clone benchmarks frame cloning
func BenchmarkFrame_Clone(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			frame := NewFrame(size)
			data := make([]byte, size)
			frame.Write(data)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = frame.Clone()
			}
		})
	}
}

// BenchmarkFrame_WriteTo benchmarks WriteTo operation
func BenchmarkFrame_WriteTo(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			frame := NewFrame(size)
			data := make([]byte, size)
			frame.Write(data)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = frame.WriteTo(io.Discard)
			}
		})
	}
}

// BenchmarkFrame_EncodeOptimized benchmarks an optimized encode path
func BenchmarkFrame_EncodeOptimized(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384, 65536}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			frame := NewFrame(size)
			data := make([]byte, size)
			frame.Write(data)

			// Pre-allocate buffer to avoid reallocation
			buf := bytes.NewBuffer(make([]byte, 0, size+8))

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				buf.Reset()
				_ = frame.encode(buf)
			}
		})
	}
}

func formatSize(size int) string {
	if size < 1024 {
		return formatInt(size) + "B"
	} else if size < 1024*1024 {
		return formatInt(size/1024) + "KB"
	} else {
		return formatInt(size/(1024*1024)) + "MB"
	}
}

func formatInt(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	var buf [10]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(buf[i+1:])
}
