package moqt

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/okdaichi/gomoqt/transport"
)

// BenchmarkTrackReader_EnqueueDequeue benchmarks group enqueue and dequeue operations
func BenchmarkTrackReader_EnqueueDequeue(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mockStream := &FakeQUICStream{}
			substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
			reader := newTrackReader("/test", "video", substr, func() {})

			// Pre-create mock receive streams
			streams := make([]transport.ReceiveStream, size)
			for i := range streams {
				mockRecvStream := &FakeQUICReceiveStream{}
				streams[i] = mockRecvStream
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				idx := i % size

				// Enqueue
				reader.enqueueGroup(GroupSequence(idx), streams[idx])

				// Accept and immediately cancel to keep the queue flowing
				group, err := reader.AcceptGroup(context.Background())
				if err == nil && group != nil {
					group.CancelRead(InternalGroupErrorCode)
				}
			}
		})
	}
}

// BenchmarkTrackReader_AcceptGroup benchmarks accepting groups with queued data
func BenchmarkTrackReader_AcceptGroup(b *testing.B) {
	mockStream := &FakeQUICStream{}
	ctx := context.Background()
	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	reader := newTrackReader("/test", "video", substr, func() {})

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		// Enqueue a group for this iteration
		mockRecvStream := &FakeQUICReceiveStream{}
		reader.enqueueGroup(GroupSequence(i), mockRecvStream)

		// Accept it immediately (non-blocking since queue has data)
		group, err := reader.AcceptGroup(ctx)
		if err == nil && group != nil {
			group.CancelRead(InternalGroupErrorCode)
		}
	}
}

// BenchmarkTrackReader_ConcurrentAccess benchmarks concurrent enqueue/dequeue operations
func BenchmarkTrackReader_ConcurrentAccess(b *testing.B) {
	concurrency := []int{2, 10, 50}

	for _, conc := range concurrency {
		b.Run(fmt.Sprintf("goroutines-%d", conc), func(b *testing.B) {
			mockStream := &FakeQUICStream{}
			ctx := context.Background()
			substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
			reader := newTrackReader("/test", "video", substr, func() {})

			// Pre-populate queue
			for i := range 100 {
				mockRecvStream := &FakeQUICReceiveStream{}
				reader.enqueueGroup(GroupSequence(i), mockRecvStream)
			}

			b.ReportAllocs()
			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(conc)

			for g := range conc {
				go func(id int) {
					defer wg.Done()
					for i := 0; i < b.N/conc; i++ {
						if id%2 == 0 {
							// Enqueue
							mockRecvStream := &FakeQUICReceiveStream{}

							reader.enqueueGroup(GroupSequence(i+id*1000), mockRecvStream)
						} else {
							// Accept and immediately cancel
							group, err := reader.AcceptGroup(ctx)
							if err == nil && group != nil {
								group.CancelRead(InternalGroupErrorCode)
							}
						}
					}
				}(g)
			}

			wg.Wait()
		})
	}
}

// BenchmarkTrackWriter_OpenGroup benchmarks opening groups
func BenchmarkTrackWriter_OpenGroup(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("groups-%d", size), func(b *testing.B) {
			mockStream := &FakeQUICStream{}

			substr := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

			var streamMu sync.Mutex
			openUniStreamFunc := func() (transport.SendStream, error) {
				streamMu.Lock()
				defer streamMu.Unlock()

				mockSendStream := &FakeQUICSendStream{}
				mockSendStream.WriteFunc = func(p []byte) (int, error) {
					return len(p), nil
				}
				return mockSendStream, nil
			}

			writer := newTrackWriter("/broadcastpath", "trackname", substr, openUniStreamFunc, func() {})

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				group, err := writer.OpenGroup()
				if err == nil && group != nil {
					_ = group.Close()
				}
			}

			b.StopTimer()
			_ = writer.Close()
		})
	}
}

// BenchmarkTrackWriter_ConcurrentOpenGroup benchmarks concurrent group opening
func BenchmarkTrackWriter_ConcurrentOpenGroup(b *testing.B) {
	concurrency := []int{2, 10, 50}

	for _, conc := range concurrency {
		b.Run(fmt.Sprintf("goroutines-%d", conc), func(b *testing.B) {
			mockStream := &FakeQUICStream{}

			substr := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

			var streamMu sync.Mutex
			openUniStreamFunc := func() (transport.SendStream, error) {
				streamMu.Lock()
				defer streamMu.Unlock()

				mockSendStream := &FakeQUICSendStream{}
				mockSendStream.WriteFunc = func(p []byte) (int, error) {
					return len(p), nil
				}
				return mockSendStream, nil
			}

			writer := newTrackWriter("/broadcastpath", "trackname", substr, openUniStreamFunc, func() {})

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					group, err := writer.OpenGroup()
					if err == nil && group != nil {
						_ = group.Close()
					}
				}
			})

			b.StopTimer()
			_ = writer.Close()
		})
	}
}

// BenchmarkTrackWriter_ActiveGroupManagement benchmarks active group map operations
func BenchmarkTrackWriter_ActiveGroupManagement(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mockStream := &FakeQUICStream{}

			substr := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

			openUniStreamFunc := func() (transport.SendStream, error) {
				mockSendStream := &FakeQUICSendStream{}
				mockSendStream.WriteFunc = func(p []byte) (int, error) {
					return len(p), nil
				}
				return mockSendStream, nil
			}

			writer := newTrackWriter("/broadcastpath", "trackname", substr, openUniStreamFunc, func() {})

			// Pre-create groups
			groups := make([]*GroupWriter, size)
			for i := range size {
				group, _ := writer.OpenGroup()
				groups[i] = group
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				idx := i % size

				// Close and re-open group
				if groups[idx] != nil {
					_ = groups[idx].Close()
				}

				group, err := writer.OpenGroup()
				if err == nil {
					groups[idx] = group
				}
			}

			b.StopTimer()
			_ = writer.Close()
		})
	}
}

// BenchmarkTrackWriter_MemoryAllocation benchmarks memory allocation for track writers
func BenchmarkTrackWriter_MemoryAllocation(b *testing.B) {
	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		mockStream := &FakeQUICStream{}

		substr := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

		openUniStreamFunc := func() (transport.SendStream, error) {
			mockSendStream := &FakeQUICSendStream{}
			return mockSendStream, nil
		}

		writer := newTrackWriter("/broadcastpath", "trackname", substr, openUniStreamFunc, func() {})

		// Open and close a group
		group, _ := writer.OpenGroup()
		if group != nil {
			_ = group.Close()
		}

		_ = writer.Close()
	}
}

// BenchmarkTrackReader_MemoryAllocation benchmarks memory allocation for track readers
func BenchmarkTrackReader_MemoryAllocation(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		mockStream := &FakeQUICStream{}
		substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
		reader := newTrackReader("/test", "video", substr, func() {})

		// Enqueue and dequeue a group
		mockRecvStream := &FakeQUICReceiveStream{}
		reader.enqueueGroup(GroupSequence(1), mockRecvStream)

		group, err := reader.AcceptGroup(context.Background())
		if err == nil && group != nil {
			group.CancelRead(InternalGroupErrorCode)
		}

		_ = reader.Close()
	}
}

// BenchmarkTrackWriter_CloseWithActiveGroups benchmarks closing with many active groups
func BenchmarkTrackWriter_CloseWithActiveGroups(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("groups-%d", size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				mockStream := &FakeQUICStream{}

				substr := newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})

				openUniStreamFunc := func() (transport.SendStream, error) {
					mockSendStream := &FakeQUICSendStream{}
					mockSendStream.WriteFunc = func(p []byte) (int, error) {
						return len(p), nil
					}
					return mockSendStream, nil
				}

				writer := newTrackWriter("/broadcastpath", "trackname", substr, openUniStreamFunc, func() {})

				// Create many active groups
				for range size {
					_, _ = writer.OpenGroup()
				}

				// Close all at once
				_ = writer.Close()
			}
		})
	}
}
