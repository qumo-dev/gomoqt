package moqt

import (
	"testing"
)

// sinkConns keeps the snapshot live so the compiler cannot elide conns().
var sinkConns []StreamConn

// BenchmarkConnManager_AddRemove measures the per-connection hot path: every
// accepted connection calls addConn on accept and removeConn on teardown, both
// under the manager mutex. The map ops are cheap, but this guards against a
// regression that adds per-connection allocation or widens the critical
// section. A baseline connection stays registered so the count never hits zero
// (which would churn the doneChan) and pollute the measurement.
func BenchmarkConnManager_AddRemove(b *testing.B) {
	cm := newConnManager()
	cm.addConn(&FakeStreamConn{}) // baseline; stays registered
	target := &FakeStreamConn{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		cm.addConn(target)
		cm.removeConn(target)
	}
}

// BenchmarkConnManager_Snapshot measures conns(), which copies the entire live
// connection map into a fresh slice under the mutex on every Server.Close /
// Shutdown. The allocation scales with connection count; the sizes below cover
// the 1..1000 range to make a regression (per-element alloc, larger slice) visible.
func BenchmarkConnManager_Snapshot(b *testing.B) {
	sizes := []int{1, 10, 100, 1000}

	for _, size := range sizes {
		b.Run("conns-"+formatInt(size), func(b *testing.B) {
			cm := newConnManager()
			for range size {
				cm.addConn(&FakeStreamConn{})
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				sinkConns = cm.conns()
			}
		})
	}
}
