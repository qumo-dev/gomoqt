package moqt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

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

			// Create a repeating reader for the benchmark
			repeatingData := bytes.Repeat(encodedData, b.N+1)
			reader := bytes.NewReader(repeatingData)

			mockStream := &MockQUICSendStream{}
			mockStream.On("Context").Return(context.Background())

			// Wrap the reader to implement ReceiveStream
			recvStream := &mockReceiveStream{Reader: reader}

			groupReader := newGroupReader(GroupSequence(1), recvStream, func() {})

			frame := NewFrame(size)

			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				frame.Reset()
				err := groupReader.ReadFrame(frame)
				if err == io.EOF {
					b.ResetTimer()
					reader = bytes.NewReader(repeatingData)
					recvStream = &mockReceiveStream{Reader: reader}
					groupReader = newGroupReader(GroupSequence(1), recvStream, func() {})
					continue
				}
				if err != nil {
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
			// Create mock send stream
			mockStream := &MockQUICSendStream{}
			mockStream.On("Context").Return(context.Background())

			// Use a discard writer to avoid memory accumulation
			sendStream := &mockSendStream{Writer: io.Discard}

			groupWriter := newGroupWriter(sendStream, GroupSequence(1), func() {})

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

			repeatingData := bytes.Repeat(encodedData, b.N*conc+1)

			b.ReportAllocs()
			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(conc)

			for range conc {
				go func() {
					defer wg.Done()

					reader := bytes.NewReader(repeatingData)
					recvStream := &mockReceiveStream{Reader: reader}
					groupReader := newGroupReader(GroupSequence(1), recvStream, func() {})
					frame := NewFrame(frameSize)

					for i := 0; i < b.N/conc; i++ {
						frame.Reset()
						err := groupReader.ReadFrame(frame)
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

					sendStream := &mockSendStream{Writer: io.Discard}
					groupWriter := newGroupWriter(sendStream, GroupSequence(1), func() {})

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
				recvStream := &mockReceiveStream{Reader: reader}
				groupReader := newGroupReader(GroupSequence(1), recvStream, func() {})

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
	repeatingData := bytes.Repeat(encodedData, b.N+1)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(repeatingData)
		recvStream := &mockReceiveStream{Reader: reader}
		groupReader := newGroupReader(GroupSequence(1), recvStream, func() {})

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
		sendStream := &mockSendStream{Writer: io.Discard}
		groupWriter := newGroupWriter(sendStream, GroupSequence(1), func() {})

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

// Mock implementations for testing

type mockReceiveStream struct {
	*bytes.Reader
}

func (m *mockReceiveStream) CancelRead(StreamErrorCode) {}
func (m *mockReceiveStream) SetReadDeadline(t time.Time) error {
	return nil
}
func (m *mockReceiveStream) StreamID() StreamID { return 0 }

type mockSendStream struct {
	io.Writer
}

func (m *mockSendStream) CancelWrite(StreamErrorCode)        {}
func (m *mockSendStream) Close() error                       { return nil }
func (m *mockSendStream) Context() context.Context           { return context.Background() }
func (m *mockSendStream) StreamID() StreamID                 { return 0 }
func (m *mockSendStream) SetWriteDeadline(t time.Time) error { return nil }
