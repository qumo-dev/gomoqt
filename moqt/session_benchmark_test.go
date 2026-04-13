package moqt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

// BenchmarkSession_Subscribe benchmarks subscribe operations
func BenchmarkSession_Subscribe(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			conn := &FakeStreamConn{}

			// Mock OpenStream to return streams that will complete the subscribe handshake
			streamIndex := 0
			conn.OpenStreamFunc = func() (transport.Stream, error) {
				mockBiStream := &FakeQUICStream{}
				streamIndex++

				// Mock Read for SUBSCRIBE_OK message
				mockBiStream.ReadFunc = func(b []byte) (int, error) {
					// Encode SUBSCRIBE_OK message
					msg := message.SubscribeOkMessage{}
					var buf bytes.Buffer
					_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
					err := msg.Encode(&buf)
					if err != nil {
						return 0, err
					}
					data := buf.Bytes()
					copy(b, data)
					return len(data), io.EOF
				}

				return mockBiStream, nil
			}

			mux := NewTrackMux()
			session := newSession(conn, mux, nil, nil, nil)

			// Pre-generate paths
			paths := make([]BroadcastPath, size)
			names := make([]TrackName, size)
			for i := range size {
				paths[i] = BroadcastPath(fmt.Sprintf("/broadcast/%d", i))
				names[i] = TrackName(fmt.Sprintf("track_%d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				idx := i % size
				_, _ = session.Subscribe(context.Background(), paths[idx], names[idx], nil)
			}

			b.StopTimer()
			_ = session.CloseWithError(NoError, "benchmark complete")
		})
	}
}

// BenchmarkSession_ConcurrentSubscribe benchmarks concurrent subscribe operations
func BenchmarkSession_ConcurrentSubscribe(b *testing.B) {
	concurrency := []int{10, 50, 100}

	for _, conc := range concurrency {
		b.Run(fmt.Sprintf("goroutines-%d", conc), func(b *testing.B) {
			conn := &FakeStreamConn{}

			var streamMu sync.Mutex
			streamIndex := 0
			conn.OpenStreamFunc = func() (transport.Stream, error) {
				streamMu.Lock()
				defer streamMu.Unlock()

				mockBiStream := &FakeQUICStream{}
				streamIndex++
				mockBiStream.ReadFunc = func(b []byte) (int, error) {
					msg := message.SubscribeOkMessage{}
					var buf bytes.Buffer
					_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
					err := msg.Encode(&buf)
					if err != nil {
						return 0, err
					}
					data := buf.Bytes()
					copy(b, data)
					return len(data), io.EOF
				}
				return mockBiStream, nil
			}

			mux := NewTrackMux()
			session := newSession(conn, mux, nil, nil, nil)

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					path := BroadcastPath(fmt.Sprintf("/broadcast/%d", i))
					name := TrackName(fmt.Sprintf("track_%d", i))
					_, _ = session.Subscribe(context.Background(), path, name, nil)
					i++
				}
			})

			b.StopTimer()
			_ = session.CloseWithError(NoError, "benchmark complete")
		})
	}
}

// BenchmarkSession_TrackReaderOperations benchmarks adding/removing track readers
func BenchmarkSession_TrackReaderOperations(b *testing.B) {
	conn := &FakeStreamConn{}

	mux := NewTrackMux()
	session := newSession(conn, mux, nil, nil, nil)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		id := SubscribeID(i)

		// Create mock subscribe stream
		mockSubStream := &FakeQUICStream{}

		substr := newSendSubscribeStream(id, mockSubStream, &SubscribeConfig{})
		trackReader := newTrackReader("/test", "video", substr, func() {})

		// Add track reader
		session.addTrackReader(id, trackReader)

		// Remove track reader
		session.removeTrackReader(id)
	}

	b.StopTimer()
	_ = session.CloseWithError(NoError, "benchmark complete")
}

// BenchmarkSession_TrackWriterOperations benchmarks adding/removing track writers
func BenchmarkSession_TrackWriterOperations(b *testing.B) {
	conn := &FakeStreamConn{}

	mux := NewTrackMux()
	session := newSession(conn, mux, nil, nil, nil)

	b.ReportAllocs()

	for i := range b.N {
		id := SubscribeID(i)

		// Create mock subscribe stream
		mockSubStream := &FakeQUICStream{}

		substr := newReceiveSubscribeStream(id, mockSubStream, &SubscribeConfig{})
		trackWriter := newTrackWriter(
			BroadcastPath("/test"),
			TrackName("track"),
			substr,
			func() (transport.SendStream, error) { return nil, nil },
			func() {},
		)

		// Add track writer
		session.addTrackWriter(id, trackWriter)

		// Remove track writer
		session.removeTrackWriter(id)
	}

	b.StopTimer()
	_ = session.CloseWithError(NoError, "benchmark complete")
}

// BenchmarkSession_MapLookup benchmarks concurrent map lookups for track readers/writers
func BenchmarkSession_MapLookup(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			conn := &FakeStreamConn{}

			mux := NewTrackMux()
			session := newSession(conn, mux, nil, nil, nil)

			// Pre-populate with track readers
			for i := range size {
				id := SubscribeID(i)
				mockSubStream := &FakeQUICStream{}

				substr := newSendSubscribeStream(id, mockSubStream, &SubscribeConfig{})
				trackReader := newTrackReader("/test", "video", substr, func() {})
				session.addTrackReader(id, trackReader)
			}

			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					id := SubscribeID(i % size)
					// Simple map access benchmark
					session.trackReaderMapLocker.RLock()
					_ = session.trackReaders[id]
					session.trackReaderMapLocker.RUnlock()
					i++
				}
			})

			b.StopTimer()
			_ = session.CloseWithError(NoError, "benchmark complete")
		})
	}
}

// BenchmarkSession_MemoryAllocation benchmarks memory allocation patterns
func BenchmarkSession_MemoryAllocation(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("readers-%d", size), func(b *testing.B) {
			b.ReportAllocs()

			for range b.N {
				conn := &FakeStreamConn{}

				mux := NewTrackMux()
				session := newSession(conn, mux, nil, nil, nil)

				// Create many track readers
				for j := range size {
					id := SubscribeID(j)
					mockSubStream := &FakeQUICStream{}

					substr := newSendSubscribeStream(id, mockSubStream, &SubscribeConfig{})
					trackReader := newTrackReader("/test", "video", substr, func() {})
					session.addTrackReader(id, trackReader)
				}

				_ = session.CloseWithError(NoError, "benchmark complete")
			}
		})
	}
}

// BenchmarkSession_ContextCancellation benchmarks session cleanup on context cancellation
func BenchmarkSession_ContextCancellation(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		ctx, cancel := context.WithCancel(context.Background())

		conn := &FakeStreamConn{}
		conn.ParentCtx = ctx

		mux := NewTrackMux()
		session := newSession(conn, mux, nil, nil, nil)

		// Cancel context
		cancel()

		// Close session
		_ = session.CloseWithError(NoError, "benchmark complete")

		// Small delay to allow goroutines to finish
		time.Sleep(time.Millisecond)
	}
}
