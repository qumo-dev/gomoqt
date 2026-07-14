package moqt

import (
	"strings"
	"testing"
)

var benchmarkResult string
var benchmarkBool bool

func BenchmarkBroadcastPath_GetSuffix_TrimPrefix(b *testing.B) {
	bc := BroadcastPath("live/camera1/video/hd")
	prefix := "live/camera1/"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !bc.HasPrefix(prefix) {
			benchmarkResult = ""
			benchmarkBool = false
		} else {
			benchmarkResult = strings.TrimPrefix(string(bc), prefix)
			benchmarkBool = true
		}
	}
}

func BenchmarkBroadcastPath_GetSuffix_Slicing(b *testing.B) {
	bc := BroadcastPath("live/camera1/video/hd")
	prefix := "live/camera1/"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !bc.HasPrefix(prefix) {
			benchmarkResult = ""
			benchmarkBool = false
		} else {
			benchmarkResult = string(bc)[len(prefix):]
			benchmarkBool = true
		}
	}
}

func BenchmarkBroadcastPath_Extension_LastIndex(b *testing.B) {
	bc := BroadcastPath("live/camera1/video/hd.mp4")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i := strings.LastIndex(string(bc), "."); i >= 0 {
			benchmarkResult = string(bc)[i:]
		} else {
			benchmarkResult = ""
		}
	}
}

func BenchmarkBroadcastPath_Extension_LastIndexByte(b *testing.B) {
	bc := BroadcastPath("live/camera1/video/hd.mp4")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i := strings.LastIndexByte(string(bc), '.'); i >= 0 {
			benchmarkResult = string(bc)[i:]
		} else {
			benchmarkResult = ""
		}
	}
}
