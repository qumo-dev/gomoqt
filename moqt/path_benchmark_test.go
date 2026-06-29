package moqt

import (
	"context"
	"strconv"
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

// BenchmarkGroupSequence_String benchmarks GroupSequence string conversion,
// which the optimized path renders via strconv.FormatUint instead of
// fmt.Sprintf. Sequence numbers span the varint width range so the formatting
// cost is measured across short and long decimal renderings.
func BenchmarkGroupSequence_String(b *testing.B) {
	seqs := []GroupSequence{
		1,
		1 << 16,
		1 << 32,
		MaxGroupSequence,
	}

	for _, seq := range seqs {
		b.Run(strconv.FormatUint(uint64(seq), 10), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkStr = seq.String()
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

// Sinks prevent the compiler from dead-code-eliminating the inlinable
// BroadcastPath method calls below — they live in the same package, so the
// compiler can prove Equal/HasPrefix are pure and drop an unused result.
var (
	pathSinkBool bool
	pathSinkStr  string
	pathSinkSegs []prefixSegment
)

// BenchmarkPrefixSegments measures prefixSegments — the sibling of pathSegments
// (benched above) that splits a slash-bracketed prefix such as "/a/b/c/" into
// segments. It was rewritten alongside pathSegments in #253 but had no
// dedicated benchmark, so a regression in the prefix-match path (run on every
// route lookup) would have been invisible. Inputs mirror pathSegments' depth
// cases, wrapped in the leading+trailing slashes prefixSegments expects.
func BenchmarkPrefixSegments(b *testing.B) {
	testCases := []struct {
		name   string
		prefix string
	}{
		{"root", "/"},
		{"single", "/segment/"},
		{"double", "/segment/path/"},
		{"triple", "/segment/path/name/"},
		{"deep-5", "/a/b/c/d/e/"},
		{"deep-10", "/a/b/c/d/e/f/g/h/i/j/"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkSegs = prefixSegments(tc.prefix)
			}
		})
	}
}

// BenchmarkBroadcastPath_HasPrefix measures prefix matching on the routing
// path (called during announcement/subscribe matching).
func BenchmarkBroadcastPath_HasPrefix(b *testing.B) {
	cases := []struct {
		name   string
		path   BroadcastPath
		prefix string
	}{
		{"match-short", "/live", "/li"},
		{"match-deep", "/live/camera1/track", "/live/camera1"},
		{"no-match", "/live/camera1", "/vod"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkBool = tc.path.HasPrefix(tc.prefix)
			}
		})
	}
}

// BenchmarkBroadcastPath_GetSuffix measures suffix extraction. GetSuffix
// converts the named type twice (once in HasPrefix, once in TrimPrefix) and
// returns a substring; this guards both the conversion and the TrimPrefix cost.
func BenchmarkBroadcastPath_GetSuffix(b *testing.B) {
	cases := []struct {
		name   string
		path   BroadcastPath
		prefix string
	}{
		{"short", "/live", "/li"},
		{"deep", "/live/camera1/track", "/live/camera1"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkStr, pathSinkBool = tc.path.GetSuffix(tc.prefix)
			}
		})
	}
}

// BenchmarkBroadcastPath_Extension measures extension extraction via a reverse
// scan for '.'.
func BenchmarkBroadcastPath_Extension(b *testing.B) {
	paths := []BroadcastPath{
		"/live/camera1",
		"/vod/movie.mp4",
		"/a/b/c/d/e/video.tar.gz",
	}

	for _, path := range paths {
		b.Run(string(path), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkStr = path.Extension()
			}
		})
	}
}

// BenchmarkBroadcastPath_Equal measures path equality — a named-type string
// compare on the hot routing path. Trivial, but inlinable, so the result is
// sunk to keep the call live.
func BenchmarkBroadcastPath_Equal(b *testing.B) {
	target := BroadcastPath("/live/camera1/track")
	paths := []BroadcastPath{
		"/live/camera1/track", // equal
		"/live/camera1",       // prefix, not equal
		"/vod/movie",          // unrelated
	}

	for _, path := range paths {
		b.Run(string(path), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pathSinkBool = path.Equal(target)
			}
		})
	}
}
