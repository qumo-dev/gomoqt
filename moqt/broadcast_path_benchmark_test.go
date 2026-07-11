package moqt

import (
	"testing"
)

var benchmarkResultSuffix string
var benchmarkResultBool bool
var benchmarkResultExtension string

func BenchmarkGetSuffix_TrimPrefix(b *testing.B) {
	path := BroadcastPath("/very/long/broadcast/path/to/some/video/stream")
	prefix := "/very/long/broadcast/path/to/some/"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkResultSuffix, benchmarkResultBool = path.GetSuffix(prefix)
	}
}

func BenchmarkExtension_LastIndex(b *testing.B) {
	path := BroadcastPath("/very/long/broadcast/path/to/some/video/stream.mp4")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkResultExtension = path.Extension()
	}
}
