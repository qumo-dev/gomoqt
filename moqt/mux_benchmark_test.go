package moqt

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/mock"
	"runtime"
	"strings"
	"testing"
)

// BenchmarkTrackMux_NewTrackMux benchmarks TrackMux creation
func BenchmarkTrackMux_NewTrackMux(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		mux := NewTrackMux()
		_ = mux
	}
}

func BenchmarkTrackMux_Handle(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-generate paths to avoid string generation overhead during benchmark
			paths := make([]BroadcastPath, size)
			for i := range size {
				paths[i] = BroadcastPath(fmt.Sprintf("/path/%d", i))
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				// Use modulo to cycle through paths for repeated benchmarks
				path := paths[i%size]
				mux.Publish(ctx, path, handler)
			}
		})
	}
}

// BenchmarkTrackMux_Handler benchmarks handler lookup
func BenchmarkTrackMux_Handler(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-populate with handlers
			paths := make([]BroadcastPath, size)
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/path/%d", i))
				paths[i] = path
				mux.Publish(ctx, path, handler)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				path := paths[i%size]
				mux.TrackHandler(path)
			}
		})
	}
}

// BenchmarkTrackMux_ServeTrack benchmarks track serving
func BenchmarkTrackMux_ServeTrack(b *testing.B) {
	mux := NewTrackMux()
	ctx := context.Background()
	path := BroadcastPath("/test/path")

	// Register a simple handler
	mux.Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {
		// Simple no-op handler for benchmarking
	}))

	// Create a test track writer
	openUniStreamFunc := func() (SendStream, error) {
		mockSendStream := &MockQUICSendStream{}
		mockSendStream.On("CancelWrite", mock.Anything).Return()
		mockSendStream.On("StreamID").Return(StreamID(1))
		mockSendStream.On("Close").Return(nil)
		mockSendStream.On("Write", mock.Anything).Return(0, nil)
		return mockSendStream, nil
	}
	onCloseTrack := func() {}
	trackWriter := newTrackWriter(path, TrackName("test_track"), nil, openUniStreamFunc, onCloseTrack)

	b.ReportAllocs()

	for b.Loop() {
		mux.serveTrack(trackWriter)
	}
}

// BenchmarkTrackMux_ServeAnnouncements benchmarks announcement setup overhead
func BenchmarkTrackMux_ServeAnnouncements(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-populate with handlers under /room/ prefix
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/room/user%d", i))
				mux.Publish(ctx, path, handler)
			}

			b.ReportAllocs()
			b.ResetTimer()

			// Benchmark announcement writer creation (not the blocking operation)
			for b.Loop() {
				mockStream := &MockQUICStream{}
				mockStream.On("Context").Return(ctx)
				mockStream.On("StreamID").Return(StreamID(1))
				mockStream.On("Write", mock.Anything).Return(0, nil)
				mockStream.On("Close").Return(nil)
				_ = newAnnouncementWriter(mockStream, "/room/")
			}
		})
	}
}

// BenchmarkTrackMux_ConcurrentRead benchmarks concurrent handler lookups
func BenchmarkTrackMux_ConcurrentRead(b *testing.B) {
	mux := NewTrackMux()
	ctx := context.Background()
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Pre-populate with handlers
	const numPaths = 1000
	paths := make([]BroadcastPath, numPaths)
	for i := range numPaths {
		path := BroadcastPath(fmt.Sprintf("/path/%d", i))
		paths[i] = path
		mux.Publish(ctx, path, handler)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := paths[i%numPaths]
			mux.TrackHandler(path)
			i++
		}
	})
}

// BenchmarkTrackMux_ConcurrentWrite benchmarks concurrent handler registration
func BenchmarkTrackMux_ConcurrentWrite(b *testing.B) {
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mux := NewTrackMux()
			ctx := context.Background()
			path := BroadcastPath(fmt.Sprintf("/path/%d", i))
			mux.Publish(ctx, path, handler)
			i++
		}
	})
}

// BenchmarkTrackMux_MixedWorkload benchmarks mixed read/write operations
func BenchmarkTrackMux_MixedWorkload(b *testing.B) {
	mux := NewTrackMux()
	ctx := context.Background()
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Pre-populate with some handlers
	const initialPaths = 500
	paths := make([]BroadcastPath, initialPaths)
	for i := range initialPaths {
		path := BroadcastPath(fmt.Sprintf("/existing/%d", i))
		paths[i] = path
		mux.Publish(ctx, path, handler)
	}

	var writeCounter int64

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		localCounter := 0
		for pb.Next() {
			if localCounter%10 == 0 {
				// 10% writes - new handler registration
				newPath := BroadcastPath(fmt.Sprintf("/new/%d", writeCounter))
				mux.Publish(ctx, newPath, handler)
				writeCounter++
			} else {
				// 90% reads - handler lookup
				path := paths[localCounter%initialPaths]
				mux.TrackHandler(path)
			}
			localCounter++
		}
	})
}

// BenchmarkTrackMux_DeepNestedPaths benchmarks performance with deeply nested paths
func BenchmarkTrackMux_DeepNestedPaths(b *testing.B) {
	depths := []int{5, 10, 20}

	for _, depth := range depths {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Create deeply nested path
			var pathBuilder strings.Builder
			pathBuilder.WriteString("/root")
			for i := range depth {
				pathBuilder.WriteString(fmt.Sprintf("/level%d", i))
			}
			path := BroadcastPath(pathBuilder.String())

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				if i%2 == 0 {
					mux.Publish(ctx, path, handler)
				} else {
					mux.TrackHandler(path)
				}
			}
		})
	}
}

// BenchmarkTrackMux_MemoryUsage benchmarks memory usage with different numbers of handlers
func BenchmarkTrackMux_MemoryUsage(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("handlers-%d", size), func(b *testing.B) {
			var m1, m2 runtime.MemStats

			b.ReportAllocs()
			runtime.GC()
			runtime.ReadMemStats(&m1)

			b.ResetTimer()

			for i := range b.N {
				mux := NewTrackMux()
				ctx := context.Background()
				handler := TrackHandlerFunc(func(tw *TrackWriter) {})

				// Register many handlers
				for j := range size {
					path := BroadcastPath(fmt.Sprintf("/path/%d/%d", i, j))
					mux.Publish(ctx, path, handler)
				}

				// Perform some operations to measure realistic memory usage
				for j := range 100 {
					lookupPath := BroadcastPath(fmt.Sprintf("/path/%d/%d", i, j%size))
					mux.TrackHandler(lookupPath)
				}
			}

			b.StopTimer()
			runtime.GC()
			runtime.ReadMemStats(&m2)

			b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "allocs/op")
		})
	}
}

// Benchmark helper that measures allocation patterns for specific operations
func BenchmarkTrackMux_AllocationPatterns(b *testing.B) {
	b.Run("handler-registration", func(b *testing.B) {
		mux := NewTrackMux()
		ctx := context.Background()
		handler := TrackHandlerFunc(func(tw *TrackWriter) {})

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := BroadcastPath(fmt.Sprintf("/alloc/test/%d", i))
			mux.Publish(ctx, path, handler)
		}
	})

	b.Run("handler-lookup", func(b *testing.B) {
		mux := NewTrackMux()
		ctx := context.Background()
		handler := TrackHandlerFunc(func(tw *TrackWriter) {})

		// Pre-register handlers
		paths := make([]BroadcastPath, 1000)
		for i := range 1000 {
			path := BroadcastPath(fmt.Sprintf("/alloc/lookup/%d", i))
			paths[i] = path
			mux.Publish(ctx, path, handler)
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := paths[i%1000]
			mux.TrackHandler(path)
		}
	})
}

// BenchmarkTrackMux_StringOperations benchmarks string operations impact
func BenchmarkTrackMux_StringOperations(b *testing.B) {
	b.Run("path-validation", func(b *testing.B) {
		paths := []BroadcastPath{
			BroadcastPath("/valid/path"),
			BroadcastPath("/another/valid/path"),
			BroadcastPath("/deep/nested/valid/path"),
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := paths[i%len(paths)]
			isValidPath(path)
		}
	})

	b.Run("prefix-validation", func(b *testing.B) {
		prefixes := []string{
			"/valid/",
			"/another/",
			"/deep/nested/",
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			prefix := prefixes[i%len(prefixes)]
			isValidPrefix(prefix)
		}
	})

	b.Run("path-splitting", func(b *testing.B) {
		paths := []BroadcastPath{
			BroadcastPath("/level1/level2/track"),
			BroadcastPath("/room/user123/video"),
			BroadcastPath("/game/match456/audio"),
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := paths[i%len(paths)]
			strings.Split(string(path), "/")
		}
	})
}

// BenchmarkTrackMux_LockContention benchmarks mutex contention scenarios
func BenchmarkTrackMux_LockContention(b *testing.B) {
	mux := NewTrackMux()
	ctx := context.Background()
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Pre-populate with handlers
	const numPaths = 1000
	for i := range numPaths {
		path := BroadcastPath(fmt.Sprintf("/path/%d", i))
		mux.Publish(ctx, path, handler)
	}

	b.Run("read-heavy", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				path := BroadcastPath(fmt.Sprintf("/path/%d", i%numPaths))
				mux.TrackHandler(path)
				i++
			}
		})
	})

	b.Run("write-heavy", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				newMux := NewTrackMux()
				path := BroadcastPath(fmt.Sprintf("/new/%d", i))
				newMux.Publish(ctx, path, handler)
				i++
			}
		})
	})

	b.Run("mixed-contention", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%20 == 0 {
					// 5% writes
					path := BroadcastPath(fmt.Sprintf("/contention/%d", i))
					mux.Publish(ctx, path, handler)
				} else {
					// 95% reads
					path := BroadcastPath(fmt.Sprintf("/path/%d", i%numPaths))
					mux.TrackHandler(path)
				}
				i++
			}
		})
	})
}

// BenchmarkTrackMux_AnnouncementTree benchmarks the announcement tree operations
func BenchmarkTrackMux_AnnouncementTree(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("tree-traversal-size-%d", size), func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Create a deep tree structure
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/level1/level2/level3/track%d", i))
				mux.Publish(ctx, path, handler)
			}

			b.ReportAllocs()
			b.ResetTimer()

			// Benchmark announcement writer creation and tree structure access
			for b.Loop() {
				mockStream := &MockQUICStream{}
				mockStream.On("Context").Return(context.Background())
				mockStream.On("Write", mock.Anything).Return(0, nil)
				mockStream.On("Close").Return(nil)
				_ = newAnnouncementWriter(mockStream, "/level1/")
			}
		})
	}
}

// BenchmarkTrackMux_MapOperations benchmarks raw map performance
func BenchmarkTrackMux_MapOperations(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("map-lookup-size-%d", size), func(b *testing.B) {
			// Create a map similar to handlerIndex
			handlerMap := make(map[BroadcastPath]TrackHandler, size)
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-populate the map
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/path/%d", i))
				handlerMap[path] = handler
			}

			// Prepare lookup paths
			lookupPaths := make([]BroadcastPath, 1000)
			for i := range 1000 {
				lookupPaths[i] = BroadcastPath(fmt.Sprintf("/path/%d", i%size))
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				path := lookupPaths[i%1000]
				_ = handlerMap[path]
			}
		})
	}
}

// BenchmarkTrackMux_GCPressure benchmarks GC impact with different allocation patterns
func BenchmarkTrackMux_GCPressure(b *testing.B) {
	b.Run("frequent-mux-creation", func(b *testing.B) {
		ctx := context.Background()
		handler := TrackHandlerFunc(func(tw *TrackWriter) {})

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			mux := NewTrackMux()
			for j := range 10 {
				path := BroadcastPath(fmt.Sprintf("/temp/%d/%d", i, j))
				mux.Publish(ctx, path, handler)
			}
			// Let mux go out of scope for GC
		}
	})

	b.Run("long-lived-mux", func(b *testing.B) {
		mux := NewTrackMux()
		ctx := context.Background()
		handler := TrackHandlerFunc(func(tw *TrackWriter) {})

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := BroadcastPath(fmt.Sprintf("/persistent/%d", i))
			mux.Publish(ctx, path, handler)

			// Periodic cleanup to simulate real usage
			// periodic cleanup removed: TrackMux.Clear() no longer exists
		}
	})
}

// BenchmarkTrackMux_CPUProfileOptimization provides specific scenarios for CPU profiling
func BenchmarkTrackMux_CPUProfileOptimization(b *testing.B) {
	if !testing.Short() {
		b.Run("cpu-hotspots", func(b *testing.B) {
			mux := NewTrackMux()
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Create a scenario that will show up clearly in CPU profiles
			const pathDepth = 10
			const pathCount = 1000

			// Register deeply nested paths
			for i := range pathCount {
				var pathBuilder strings.Builder
				for depth := range pathDepth {
					pathBuilder.WriteString(fmt.Sprintf("/level%d", depth))
				}
				pathBuilder.WriteString(fmt.Sprintf("/track%d", i))
				path := BroadcastPath(pathBuilder.String())
				mux.Publish(ctx, path, handler)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				// Mix of operations that will show up in CPU profile
				pathIndex := i % pathCount
				var pathBuilder strings.Builder
				for depth := range pathDepth {
					pathBuilder.WriteString(fmt.Sprintf("/level%d", depth))
				}
				pathBuilder.WriteString(fmt.Sprintf("/track%d", pathIndex))
				path := BroadcastPath(pathBuilder.String())

				// Operations that will consume CPU cycles
				mux.TrackHandler(path) // Map lookup
				trackWriter := newTrackWriter(path, TrackName(fmt.Sprintf("track-%d", i)), nil, func() (SendStream, error) {
					mockSendStream := &MockQUICSendStream{}
					mockSendStream.On("CancelWrite", mock.Anything).Return()
					mockSendStream.On("StreamID").Return(StreamID(1))
					mockSendStream.On("Close").Return(nil)
					mockSendStream.On("Write", mock.Anything).Return(0, nil)
					return mockSendStream, nil
				}, func() {})
				mux.serveTrack(trackWriter)
			}
		})
	}
}
