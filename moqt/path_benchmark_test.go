package moqt

import (
	"context"
	"strings"
	"testing"
)

// BenchmarkPathSegments benchmarks the pathSegments function with various path depths
func BenchmarkPathSegments(b *testing.B) {
	testCases := []struct {
		name string
		path BroadcastPath
	}{
		{"root", "/"},
		{"single", "/segment"},
		{"double", "/segment/path"},
		{"triple", "/segment/path/name"},
		{"deep-5", "/a/b/c/d/e"},
		{"deep-10", "/a/b/c/d/e/f/g/h/i/j"},
		{"deep-20", "/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = pathSegments(tc.path)
			}
		})
	}
}

// BenchmarkIsValidPath benchmarks path validation
func BenchmarkIsValidPath(b *testing.B) {
	testCases := []struct {
		name string
		path BroadcastPath
	}{
		{"valid-short", "/valid"},
		{"valid-medium", "/valid/path/name"},
		{"valid-long", "/very/long/valid/path/with/many/segments"},
		{"empty", ""},
		{"no-slash", "noSlash"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = isValidPath(tc.path)
			}
		})
	}
}

// BenchmarkBroadcastPath_String benchmarks BroadcastPath string conversion
func BenchmarkBroadcastPath_String(b *testing.B) {
	paths := []BroadcastPath{
		"/short",
		"/medium/length/path",
		"/very/long/broadcast/path/with/many/segments/for/testing",
	}

	for _, path := range paths {
		b.Run(string(path), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = string(path)
			}
		})
	}
}

// BenchmarkAnnouncingNode_GetChild benchmarks getting child nodes
func BenchmarkAnnouncingNode_GetChild(b *testing.B) {
	node := &announcingNode{
		children:      make(map[prefixSegment]*announcingNode),
		subscriptions: make(map[*AnnouncementWriter](chan *Announcement)),
		announcements: make(map[*Announcement]struct{}),
	}

	// Pre-populate with children
	segments := []prefixSegment{"a", "b", "c", "d", "e"}
	for _, seg := range segments {
		node.getChild(seg)
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		seg := segments[i%len(segments)]
		_ = node.getChild(seg)
	}
}

// BenchmarkAnnouncingNode_AddAnnouncement benchmarks adding announcements to nodes
func BenchmarkAnnouncingNode_AddAnnouncement(b *testing.B) {
	sizes := []int{1, 10, 100}

	for _, size := range sizes {
		b.Run(formatInt(size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				b.StopTimer()
				node := &announcingNode{
					children:      make(map[prefixSegment]*announcingNode),
					subscriptions: make(map[*AnnouncementWriter](chan *Announcement)),
					announcements: make(map[*Announcement]struct{}),
				}
				ctx := context.Background()

				// Pre-create announcements
				announcements := make([]*Announcement, size)
				for j := range size {
					ann, _ := NewAnnouncement(ctx, BroadcastPath("/test"))
					announcements[j] = ann
				}
				b.StartTimer()

				// Add all announcements
				for _, ann := range announcements {
					node.addAnnouncement(ann)
				}
			}
		})
	}
}

// BenchmarkTrackMux_FindTrackHandler benchmarks handler lookup
func BenchmarkTrackMux_FindTrackHandler(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run(formatInt(size), func(b *testing.B) {
			mux := NewTrackMux(0)
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Pre-populate with handlers
			paths := make([]BroadcastPath, size)
			for i := range size {
				path := BroadcastPath("/path/" + formatInt(i))
				paths[i] = path
				mux.Publish(ctx, path, handler)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				path := paths[i%size]
				_ = mux.findTrackHandler(path)
			}
		})
	}
}

// BenchmarkTrackMux_RegisterHandler benchmarks handler registration
func BenchmarkTrackMux_RegisterHandler(b *testing.B) {
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		mux := NewTrackMux(0)
		ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/path"))
		b.StartTimer()

		_ = mux.registerHandler(ann, handler)
	}
}

// BenchmarkTrackMux_RemoveHandler benchmarks handler removal
func BenchmarkTrackMux_RemoveHandler(b *testing.B) {
	handler := TrackHandlerFunc(func(tw *TrackWriter) {})

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		mux := NewTrackMux(0)
		ctx := context.Background()
		ann, _ := NewAnnouncement(ctx, BroadcastPath("/test/path"))
		announced := mux.registerHandler(ann, handler)
		b.StartTimer()

		mux.removeHandler(announced)
	}
}

// BenchmarkTrackMux_PathTraversal benchmarks tree traversal for announcements
func BenchmarkTrackMux_PathTraversal(b *testing.B) {
	depths := []int{1, 3, 5, 10}

	for _, depth := range depths {
		b.Run("depth-"+formatInt(depth), func(b *testing.B) {
			mux := NewTrackMux(0)
			ctx := context.Background()
			handler := TrackHandlerFunc(func(tw *TrackWriter) {})

			// Create a path with the specified depth
			var path strings.Builder
			path.WriteString("/")
			for i := range depth {
				path.WriteString("seg" + formatInt(i))
				if i < depth-1 {
					path.WriteString("/")
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ann, end := NewAnnouncement(ctx, BroadcastPath(path.String()))
				mux.Announce(ann, handler)
				end()
			}
		})
	}
}
