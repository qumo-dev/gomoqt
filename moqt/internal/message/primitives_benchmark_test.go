package message

import (
	"bytes"
	"testing"
)

// This file benchmarks the per-field wire coders (message_reader.go /
// message_writer.go) in isolation. They are the hottest pure-compute path in
// the library — every field on every message flows through them — yet they had
// no direct benchmarks, which is why optimization PRs such as #198 (ReadString
// via unsafe.String) and #214 (WriteString) could not be measured directly and
// the benchmark workflow posted "No significant changes". The composite
// *Message_Decode benches blur these costs together; these isolate them so a
// future change to one coder can be attributed precisely.
//
// Measurement notes (the suite's recurring traps, guarded against here):
//   - Inputs are pre-encoded ONCE outside the timer into a fixed-size tile, so
//     the input buffer never scales with b.N (the #213 bytes.Repeat(b, b.N+1)
//     trap that OOMs CI). Read coders advance an offset into the tile rather
//     than allocating per iteration.
//   - Write coders reuse one destination buffer (reset to [:0] each iteration),
//     so append() never grows it (the #192 growable-buffer trap).
//   - Every return value is sunk into a package-level var so the compiler
//     cannot dead-code-eliminate the measured call.

var (
	sinkUint64 uint64
	sinkStr    string
	sinkSlice  []byte
	sinkArr    []string
	sinkInt    int
)

// tile replicates b into a ~64 KiB buffer. The size is a CONSTANT (does not
// scale with b.N), so unlike bytes.Repeat(b, b.N+1) it cannot OOM or inflate GC
// pressure as the benchmark iterates more.
func tile(b []byte) []byte {
	const budget = 1 << 16 // 64 KiB
	return bytes.Repeat(b, budget/len(b)+1)
}

// --- Read side ---

// BenchmarkReadVarint measures varint decoding at each of the four legal widths.
// The input for each width is pre-encoded once and tiled; the loop only slices
// and advances an offset (no allocation), sinking the decoded value + length so
// the decode cannot be eliminated.
func BenchmarkReadVarint(b *testing.B) {
	widths := []struct {
		name string
		val  uint64
	}{
		{"1byte", maxVarInt1}, // top-2-bits 0b00
		{"2byte", maxVarInt2}, // top-2-bits 0b01
		{"4byte", maxVarInt4}, // top-2-bits 0b10
		{"8byte", maxVarInt8}, // top-2-bits 0b11
	}
	for _, w := range widths {
		b.Run(w.name, func(b *testing.B) {
			enc := make([]byte, 0, 8)
			enc, _ = WriteVarint(enc, w.val)
			repeating := tile(enc)

			b.ReportAllocs()
			b.SetBytes(int64(len(enc)))
			b.ResetTimer()

			off := 0
			for b.Loop() {
				if off+len(enc) > len(repeating) {
					off = 0
				}
				v, n, err := ReadVarint(repeating[off:])
				if err != nil {
					b.Fatal(err)
				}
				sinkUint64 = v
				sinkInt = n
				off += n
			}
		})
	}
}

// BenchmarkReadBytes measures length-prefixed byte-slice decoding. ReadBytes
// returns an aliased sub-slice of the input (b[:num]), so the expected cost is
// 0 allocs/op; the returned slice is sunk so the slice + prefix decode stay live.
func BenchmarkReadBytes(b *testing.B) {
	payload := []byte("hello-world-payload") // 19 bytes -> 1-byte varint prefix
	enc := make([]byte, 0, len(payload)+1)
	enc, _ = WriteBytes(enc, payload)
	repeating := tile(enc)

	b.ReportAllocs()
	b.SetBytes(int64(len(enc)))
	b.ResetTimer()

	off := 0
	for b.Loop() {
		if off+len(enc) > len(repeating) {
			off = 0
		}
		bs, n, err := ReadBytes(repeating[off:])
		if err != nil {
			b.Fatal(err)
		}
		sinkSlice = bs
		sinkInt = n
		off += n
	}
}

// BenchmarkReadString measures ReadString. ReadString does string(str) on the
// aliased sub-slice, which COPIES the bytes into a new backing array, so the
// expected cost is 1 alloc/op. This is the allocation #198 (unsafe.String)
// tried to remove; sinking the string into sinkStr also defeats any
// escape-to-stack optimization that could hide the copy.
func BenchmarkReadString(b *testing.B) {
	payload := []byte("hello-world-payload")
	enc := make([]byte, 0, len(payload)+1)
	enc, _ = WriteBytes(enc, payload)
	repeating := tile(enc)

	b.ReportAllocs()
	b.SetBytes(int64(len(enc)))
	b.ResetTimer()

	off := 0
	for b.Loop() {
		if off+len(enc) > len(repeating) {
			off = 0
		}
		s, n, err := ReadString(repeating[off:])
		if err != nil {
			b.Fatal(err)
		}
		sinkStr = s
		sinkInt = n
		off += n
	}
}

// BenchmarkReadStringArray measures count-prefixed string-array decoding.
// ReadStringArray allocates the backing slice (make([]string, 0, count)) plus
// one copied string per element, so the expected cost is >=2 allocs/op for a
// non-degenerate array. A 3-element array is used so the result is
// representative rather than the degenerate 0/1-element case.
func BenchmarkReadStringArray(b *testing.B) {
	arr := []string{"alpha", "beta", "gamma"}
	enc := make([]byte, 0, 32)
	enc, _ = WriteStringArray(enc, arr)
	repeating := tile(enc)

	b.ReportAllocs()
	b.SetBytes(int64(len(enc)))
	b.ResetTimer()

	off := 0
	for b.Loop() {
		if off+len(enc) > len(repeating) {
			off = 0
		}
		a, n, err := ReadStringArray(repeating[off:])
		if err != nil {
			b.Fatal(err)
		}
		sinkArr = a
		sinkInt = n
		off += n
	}
}

// --- Write side ---
//
// All write coders reuse a single destination buffer, reset to [:0] each
// iteration so append() never grows it. b.SetBytes uses a one-time probe
// outside the timer for the exact encoded length (not derived from the value's
// bit pattern, which is error-prone). []byte(s) inside WriteString is a free
// no-op conversion per escape analysis, so these report 0 allocs/op — the
// asymmetry with ReadString's 1 alloc/op is the signal these benches preserve.

func BenchmarkWriteVarint(b *testing.B) {
	widths := []struct {
		name string
		val  uint64
	}{
		{"1byte", maxVarInt1},
		{"2byte", maxVarInt2},
		{"4byte", maxVarInt4},
		{"8byte", maxVarInt8},
	}
	for _, w := range widths {
		b.Run(w.name, func(b *testing.B) {
			probe := make([]byte, 0, 8)
			_, encLen := WriteVarint(probe, w.val)

			dest := make([]byte, 0, 8) // cap covers the widest single varint

			b.ReportAllocs()
			b.SetBytes(int64(encLen))
			b.ResetTimer()

			for b.Loop() {
				dest = dest[:0]
				out, n := WriteVarint(dest, w.val)
				sinkInt = n
				sinkSlice = out
			}
		})
	}
}

func BenchmarkWriteBytes(b *testing.B) {
	payload := []byte("hello-world-payload")
	probe := make([]byte, 0, len(payload)+1)
	_, encLen := WriteBytes(probe, payload)

	dest := make([]byte, 0, len(payload)+8)

	b.ReportAllocs()
	b.SetBytes(int64(encLen))
	b.ResetTimer()

	for b.Loop() {
		dest = dest[:0]
		out, n := WriteBytes(dest, payload)
		sinkInt = n
		sinkSlice = out
	}
}

func BenchmarkWriteString(b *testing.B) {
	s := "hello-world-payload"
	probe := make([]byte, 0, len(s)+1)
	_, encLen := WriteString(probe, s)

	dest := make([]byte, 0, len(s)+8)

	b.ReportAllocs()
	b.SetBytes(int64(encLen))
	b.ResetTimer()

	for b.Loop() {
		dest = dest[:0]
		out, n := WriteString(dest, s)
		sinkInt = n
		sinkSlice = out
	}
}

func BenchmarkWriteStringArray(b *testing.B) {
	arr := []string{"alpha", "beta", "gamma"}
	probe := make([]byte, 0, 64)
	_, encLen := WriteStringArray(probe, arr)

	dest := make([]byte, 0, 64) // 1-byte count + 3*(1-byte len + <=5-byte str) = 22 bytes <= 64

	b.ReportAllocs()
	b.SetBytes(int64(encLen))
	b.ResetTimer()

	for b.Loop() {
		dest = dest[:0]
		out, n := WriteStringArray(dest, arr)
		sinkInt = n
		sinkSlice = out
	}
}
