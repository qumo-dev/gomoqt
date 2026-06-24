package moqt

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/qumo-dev/gomoqt/transport"
)

// BenchmarkTrackMux_NewTrackMux benchmarks TrackMux creation
func BenchmarkTrackMux_NewTrackMux(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		mux := NewTrackMux(0)
		_ = mux
	}
}

func BenchmarkTrackMux_Handle(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mux := NewTrackMux(0)
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
			mux := NewTrackMux(0)
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
	mux := NewTrackMux(0)
	ctx := context.Background()
	path := BroadcastPath("/test/path")

	// Register a simple handler
	mux.Publish(ctx, path, TrackHandlerFunc(func(tw *TrackWriter) {
		// Simple no-op handler for benchmarking
	}))

	// Create a test track writer
	openUniStreamFunc := func(_ context.Context) (transport.SendStream, error) {
		mockSendStream := &FakeQUICSendStream{}
		return mockSendStream, nil
	}
	onCloseTrack := func() {}
	trackWriter := &TrackWriter{
		BroadcastPath:     path,
		TrackName:         TrackName("test_track"),
		openUniStreamFunc: openUniStreamFunc,
		onCloseTrackFunc:  onCloseTrack,
	}

	b.ReportAllocs()

	for b.Loop() {
		mux.serveTrack(trackWriter)
	}
}

// BenchmarkTrackMux_ServeAnnouncements measures the real announcement fan-out
// pipeline end to end: each iteration Publishes a new track whose path matches
// a registered AnnouncementWriter's prefix, which triggers
// Announce -> announcement-tree walk -> per-node subscription snapshot ->
// channel send -> SendAnnouncement -> AnnounceMessage encode on the writer's
// stream.
//
// The previous version constructed an AnnouncementWriter per iteration but
// never invoked Announce/Publish, so it measured only allocation and none of
// the fan-out.
//
// A long-lived AnnouncementWriter is registered via serveAnnouncements (run in
// a background goroutine). serveAnnouncements' subscription channel has a
// fixed buffer of 8 and, when full, the production code drops the subscription
// and closes the writer. To measure the steady-state fan-out (channel send
// succeeding) rather than the drop/close path, each iteration waits for the
// consumer to acknowledge delivery (via a WriteFunc signal) before publishing
// the next announcement. This yields a stable per-operation latency for the
// full relay forwarding path.
func BenchmarkTrackMux_ServeAnnouncements(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			mux := NewTrackMux(0)
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-populate with handlers under /room/ prefix so the tree has depth.
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/room/user%d", i))
				mux.Publish(ctx, path, handler)
			}

			awCtx, cancelAW := context.WithCancel(ctx)
			// delivered is signaled on every Write to the writer's stream (during
			// init AND on each forwarded announcement). Used both to wait for
			// init completion before timing and to acknowledge each fan-out.
			delivered := make(chan struct{}, 1)
			mockStream := &FakeQUICStream{
				ParentCtx: awCtx,
				WriteFunc: func(p []byte) (int, error) {
					select {
					case delivered <- struct{}{}:
					default:
					}
					return len(p), nil
				},
			}
			aw := newAnnouncementWriter(mockStream, "/room/", 0, 0, nil)

			var awWG sync.WaitGroup
			awWG.Add(1)
			go func() {
				defer awWG.Done()
				mux.serveAnnouncements(aw)
			}()

			// Block until init has fully completed. The fan-out path in
			// Announce does a non-blocking channel send and, on overflow, closes
			// the writer (setting aw.actives = nil); if a Publish raced init's
			// registerEndHandler it would nil-deref. initDone is closed at the
			// end of the writer's one-shot init, so waiting on it guarantees no
			// concurrent Publish until init is done.
			select {
			case <-aw.initDone:
			case <-awCtx.Done():
			}

			b.Cleanup(func() {
				cancelAW()
				awWG.Wait()
			})

			b.ReportAllocs()
			b.ResetTimer()

			// Each iteration Publishes a new matching track and waits for the
			// consumer to encode the forwarded announcement, measuring one full
			// announce -> deliver round trip.
			for i := 0; b.Loop(); i++ {
				path := BroadcastPath(fmt.Sprintf("/room/bench%d", i))
				mux.Publish(ctx, path, handler)
				<-delivered
			}
		})
	}
}

// BenchmarkTrackMux_ConcurrentRead benchmarks concurrent handler lookups
func BenchmarkTrackMux_ConcurrentRead(b *testing.B) {
	mux := NewTrackMux(0)
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
			mux := NewTrackMux(0)
			ctx := context.Background()
			path := BroadcastPath(fmt.Sprintf("/path/%d", i))
			mux.Publish(ctx, path, handler)
			i++
		}
	})
}

// BenchmarkTrackMux_MixedWorkload benchmarks mixed read/write operations
func BenchmarkTrackMux_MixedWorkload(b *testing.B) {
	mux := NewTrackMux(0)
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
			mux := NewTrackMux(0)
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
				mux := NewTrackMux(0)
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
		mux := NewTrackMux(0)
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
		mux := NewTrackMux(0)
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

// Sinks for the StringOperations routing-parser sub-benches, so the compiler
// cannot eliminate the real prefixSegments/pathSegments calls.
var (
	sinkSegs []prefixSegment
	sinkLast string
)

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

	// The two sub-benches below call the REAL routing parsers from mux.go
	// (the optimized strings.IndexByte path and the production strings.Split
	// path), not a stand-in. These are the functions #220 targeted; measuring
	// them directly gives a baseline for future changes to the routing hot path.
	b.Run("prefixSegments", func(b *testing.B) {
		// prefixSegments strips [1:len-1], so inputs must be prefix-shaped
		// (leading + trailing slash).
		prefixes := []string{
			"/level1/level2/",
			"/room/user123/",
			"/game/match456/audio/",
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			prefix := prefixes[i%len(prefixes)]
			sinkSegs = prefixSegments(prefix)
		}
	})

	b.Run("pathSegments", func(b *testing.B) {
		paths := []BroadcastPath{
			BroadcastPath("/level1/level2/track"),
			BroadcastPath("/room/user123/video"),
			BroadcastPath("/game/match456/audio"),
		}

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; b.Loop(); i++ {
			path := paths[i%len(paths)]
			segs, last := pathSegments(path)
			sinkSegs = segs
			sinkLast = last
		}
	})
}

// BenchmarkTrackMux_LockContention benchmarks mutex contention scenarios
func BenchmarkTrackMux_LockContention(b *testing.B) {
	mux := NewTrackMux(0)
	ctx := context.Background()
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	// Pre-populate handlers and a matching read-path pool. Path strings are
	// built ONCE here (outside every measured loop) so alloc/CPU/GC profiles
	// reflect mux cost, not fmt.Sprintf string construction in the hot loop.
	const numPaths = 1000
	readPaths := make([]BroadcastPath, numPaths)
	for i := range numPaths {
		readPaths[i] = BroadcastPath("/path/" + strconv.Itoa(i))
		mux.Publish(ctx, readPaths[i], handler)
	}

	b.Run("read-heavy", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				mux.TrackHandler(readPaths[i%numPaths])
				i++
			}
		})
	})

	// write-heavy exercises concurrent Publish calls against the SAME shared
	// TrackMux so the mux.mu write lock is genuinely contended. Paths cycle
	// through a fixed pool (re-registration is safe: registerHandler replaces
	// the indexed handler and ends the previous one), keeping the measured
	// loop allocation-free so it reports pure lock+map cost.
	b.Run("write-heavy", func(b *testing.B) {
		const writePool = 8192
		writePaths := make([]BroadcastPath, writePool)
		for i := range writePaths {
			writePaths[i] = BroadcastPath("/new/" + strconv.Itoa(i))
		}
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				mux.Publish(ctx, writePaths[i%writePool], handler)
				i++
			}
		})
	})

	b.Run("mixed-contention", func(b *testing.B) {
		const writePool = 8192
		writePaths := make([]BroadcastPath, writePool)
		for i := range writePaths {
			writePaths[i] = BroadcastPath("/contention/" + strconv.Itoa(i))
		}
		b.ReportAllocs()
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%20 == 0 {
					// 5% writes
					mux.Publish(ctx, writePaths[i%writePool], handler)
				} else {
					// 95% reads
					mux.TrackHandler(readPaths[i%numPaths])
				}
				i++
			}
		})
	})
}

// BenchmarkTrackMux_AnnouncementTree measures the announcement-tree fan-out
// across a deep tree. A writer is registered at a shallow prefix ("/level1/")
// so each Publish of a deeply-nested track must add the announcement to every
// node along the root->leaf path and send to the subscriber snapshot at each
// node. The previous version only constructed an AnnouncementWriter and never
// exercised Announce/Publish, so the per-node walk was never measured.
func BenchmarkTrackMux_AnnouncementTree(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("tree-traversal-size-%d", size), func(b *testing.B) {
			mux := NewTrackMux(0)
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Create a deep tree structure.
			for i := range size {
				path := BroadcastPath(fmt.Sprintf("/level1/level2/level3/track%d", i))
				mux.Publish(ctx, path, handler)
			}

			// Register a writer at the shallow "/level1/" prefix so each new
			// deeply-nested Publish walks root -> level1 -> level2 -> level3 ->
			// leaf and fans out at each node. delivered acknowledges every
			// Write so the benchmark can wait for init to flush and then
			// synchronize each fan-out round trip (see ServeAnnouncements for
			// why this avoids the overflow -> drop/close path).
			awCtx, cancelAW := context.WithCancel(ctx)
			delivered := make(chan struct{}, 1)
			mockStream := &FakeQUICStream{
				ParentCtx: awCtx,
				WriteFunc: func(p []byte) (int, error) {
					select {
					case delivered <- struct{}{}:
					default:
					}
					return len(p), nil
				},
			}
			aw := newAnnouncementWriter(mockStream, "/level1/", 0, 0, nil)

			var awWG sync.WaitGroup
			awWG.Add(1)
			go func() {
				defer awWG.Done()
				mux.serveAnnouncements(aw)
			}()

			// Block until init completes; see ServeAnnouncements for why this
			// prevents a Publish from racing init's registerEndHandler.
			select {
			case <-aw.initDone:
			case <-awCtx.Done():
			}

			b.Cleanup(func() {
				cancelAW()
				awWG.Wait()
			})

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; b.Loop(); i++ {
				path := BroadcastPath(fmt.Sprintf("/level1/level2/level3/bench%d", i))
				mux.Publish(ctx, path, handler)
				<-delivered
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
			mux := NewTrackMux(0)
			for j := range 10 {
				path := BroadcastPath(fmt.Sprintf("/temp/%d/%d", i, j))
				mux.Publish(ctx, path, handler)
			}
			// Let mux go out of scope for GC
		}
	})

	b.Run("long-lived-mux", func(b *testing.B) {
		mux := NewTrackMux(0)
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
			mux := NewTrackMux(0)
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
				trackWriter := &TrackWriter{
					BroadcastPath: path,
					TrackName:     TrackName(fmt.Sprintf("track-%d", i)),
					openUniStreamFunc: func(_ context.Context) (transport.SendStream, error) {
						mockSendStream := &FakeQUICSendStream{}
						return mockSendStream, nil
					},
				}
				mux.serveTrack(trackWriter)
			}
		})
	}
}
