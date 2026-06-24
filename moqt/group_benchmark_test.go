package moqt

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
)

func newBenchmarkReceiveStream(reader io.Reader) *FakeQUICReceiveStream {
	return &FakeQUICReceiveStream{ReadFunc: reader.Read}
}

func newBenchmarkSendStream(writer io.Writer) *FakeQUICSendStream {
	return &FakeQUICSendStream{WriteFunc: writer.Write}
}

// loopReader is an io.Reader that yields src's bytes forever, wrapping around
// with no allocation and no EOF. It replaces the bytes.Repeat(src, b.N+1)
// pattern in benchmarks whose hot loop cannot tolerate the per-drain cost of a
// reader running dry: instead of a b.N-proportional input buffer (which OOMs
// CI on slow runners and inflates sec/op via GC scan of a huge live heap), the
// input is a single copy of src, repeated on demand. src must be non-empty.
type loopReader struct {
	src []byte
	off int
}

func (r *loopReader) Read(p []byte) (int, error) {
	if len(p) == 0 || len(r.src) == 0 {
		return 0, nil
	}
	n := copy(p, r.src[r.off:])
	for n < len(p) {
		n += copy(p[n:], r.src)
	}
	r.off = (r.off + n) % len(r.src)
	return n, nil
}

// BenchmarkGroupReader_ReadFrame benchmarks reading frames from a group
func BenchmarkGroupReader_ReadFrame(b *testing.B) {
	frameSizes := []int{64, 1024, 16384, 65536} // Different frame sizes

	for _, size := range frameSizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			// Create a mock receive stream with pre-encoded frame data
			frameData := make([]byte, size)
			for i := range frameData {
				frameData[i] = byte(i % 256)
			}

			// Create a frame and encode it to get the wire format
			testFrame := NewFrame(size)
			testFrame.Write(frameData)

			var buf bytes.Buffer
			_ = testFrame.encode(&buf)
			encodedData := buf.Bytes()

			// loopReader yields the encoded frame forever with no allocation
			// and no EOF, so the hot loop needs neither a b.N-proportional
			// input buffer (bytes.Repeat(encodedData, b.N+1), which OOMs CI)
			// nor the stream/groupReader rebuild + mid-loop b.ResetTimer() the
			// EOF path used (which polluted allocs/op with rebuild allocations).
			reader := &loopReader{src: encodedData}

			// Wrap the reader to implement ReceiveStream
			recvStream := newBenchmarkReceiveStream(reader)

			groupReader := newGroupReader(GroupSequence(1), recvStream, nil)

			frame := NewFrame(size)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame.Reset()
				if err := groupReader.ReadFrame(frame); err != nil {
					b.Fatalf("ReadFrame failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkGroupWriter_WriteFrame benchmarks writing frames to a group
func BenchmarkGroupWriter_WriteFrame(b *testing.B) {
	frameSizes := []int{64, 1024, 16384, 65536}

	for _, size := range frameSizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			// Use a discard writer to avoid memory accumulation
			sendStream := newBenchmarkSendStream(io.Discard)

			groupWriter := newGroupWriter(sendStream, GroupSequence(1), newGroupWriterManager())

			// Pre-create frame with data
			frame := NewFrame(size)
			frameData := make([]byte, size)
			for i := range frameData {
				frameData[i] = byte(i % 256)
			}
			frame.Write(frameData)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				err := groupWriter.WriteFrame(frame)
				if err != nil {
					b.Fatalf("WriteFrame failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkGroupReader_ConcurrentRead benchmarks concurrent frame reading
func BenchmarkGroupReader_ConcurrentRead(b *testing.B) {
	concurrency := []int{2, 10, 50}

	for _, conc := range concurrency {
		b.Run(fmt.Sprintf("goroutines-%d", conc), func(b *testing.B) {
			frameSize := 1024
			frameData := make([]byte, frameSize)

			testFrame := NewFrame(frameSize)
			testFrame.Write(frameData)

			var buf bytes.Buffer
			_ = testFrame.encode(&buf)
			encodedData := buf.Bytes()

			b.ReportAllocs()
			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(conc)

			for range conc {
				go func() {
					defer wg.Done()

					// Each goroutine gets its own loopReader (one frame,
					// repeated forever, no allocation, no EOF), replacing the
					// b.N*conc+1 shared buffer that scaled with b.N and OOM'd CI.
					reader := &loopReader{src: encodedData}
					recvStream := newBenchmarkReceiveStream(reader)
					groupReader := newGroupReader(GroupSequence(1), recvStream, nil)
					frame := NewFrame(frameSize)

					for i := 0; i < b.N/conc; i++ {
						frame.Reset()
						if err := groupReader.ReadFrame(frame); err != nil {
							return
						}
					}
				}()
			}

			wg.Wait()
		})
	}
}

// BenchmarkGroupWriter_ConcurrentWrite benchmarks concurrent frame writing
func BenchmarkGroupWriter_ConcurrentWrite(b *testing.B) {
	concurrency := []int{2, 10, 50}

	for _, conc := range concurrency {
		b.Run(fmt.Sprintf("goroutines-%d", conc), func(b *testing.B) {
			frameSize := 1024
			frameData := make([]byte, frameSize)

			b.ReportAllocs()
			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(conc)

			for range conc {
				go func() {
					defer wg.Done()

					sendStream := newBenchmarkSendStream(io.Discard)
					groupWriter := newGroupWriter(sendStream, GroupSequence(1), newGroupWriterManager())

					frame := NewFrame(frameSize)
					frame.Write(frameData)

					for i := 0; i < b.N/conc; i++ {
						err := groupWriter.WriteFrame(frame)
						if err != nil {
							return
						}
					}
				}()
			}

			wg.Wait()
		})
	}
}

// BenchmarkFrame_EncodeDecodeCycle benchmarks the full encode/decode cycle
func BenchmarkFrame_EncodeDecodeCycle(b *testing.B) {
	frameSizes := []int{64, 1024, 16384}

	for _, size := range frameSizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			frameData := make([]byte, size)
			for i := range frameData {
				frameData[i] = byte(i % 256)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				// Encode
				writeFrame := NewFrame(size)
				writeFrame.Write(frameData)

				var buf bytes.Buffer
				err := writeFrame.encode(&buf)
				if err != nil {
					b.Fatal(err)
				}

				// Decode
				readFrame := NewFrame(size)
				reader := bytes.NewReader(buf.Bytes())
				recvStream := newBenchmarkReceiveStream(reader)
				groupReader := newGroupReader(GroupSequence(1), recvStream, nil)

				err = groupReader.ReadFrame(readFrame)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGroupReader_MemoryAllocation benchmarks memory allocation in group readers
func BenchmarkGroupReader_MemoryAllocation(b *testing.B) {
	frameSize := 1024
	frameData := make([]byte, frameSize)

	testFrame := NewFrame(frameSize)
	testFrame.Write(frameData)

	var buf bytes.Buffer
	_ = testFrame.encode(&buf)
	encodedData := buf.Bytes()
	// Each iteration reads exactly one frame from a fresh stream, so a single
	// loopReader (one frame, no EOF) suffices — replacing
	// bytes.Repeat(encodedData, b.N+1), which allocated a b.N-proportional
	// buffer whose tail was never read.

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader := &loopReader{src: encodedData}
		recvStream := newBenchmarkReceiveStream(reader)
		groupReader := newGroupReader(GroupSequence(1), recvStream, nil)

		frame := NewFrame(frameSize)
		_ = groupReader.ReadFrame(frame)
	}
}

// BenchmarkGroupWriter_MemoryAllocation benchmarks memory allocation in group writers
func BenchmarkGroupWriter_MemoryAllocation(b *testing.B) {
	frameSize := 1024
	frameData := make([]byte, frameSize)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sendStream := newBenchmarkSendStream(io.Discard)
		groupWriter := newGroupWriter(sendStream, GroupSequence(1), newGroupWriterManager())

		frame := NewFrame(frameSize)
		frame.Write(frameData)
		_ = groupWriter.WriteFrame(frame)
	}
}

// BenchmarkFrame_ReuseVsAllocate benchmarks frame reuse vs new allocation
func BenchmarkFrame_ReuseVsAllocate(b *testing.B) {
	frameSize := 1024
	frameData := make([]byte, frameSize)

	b.Run("reuse", func(b *testing.B) {
		frame := NewFrame(frameSize)

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			frame.Reset()
			frame.Write(frameData)
		}
	})

	b.Run("allocate", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			frame := NewFrame(frameSize)
			frame.Write(frameData)
			_ = frame
		}
	})
}
