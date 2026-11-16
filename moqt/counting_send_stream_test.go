package moqt

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

// countingSendStream wraps a transport.SendStream to instrument Write calls
// without changing write/buffering behavior. It counts the number of Write
// calls and buckets them by payload size, and tracks total bytes written.
//
// This is a measurement/instrumentation helper. It performs NO buffering,
// NO batching, and NO change to the wrapped stream's behavior beyond routing
// each Write through a counter. It is safe for concurrent use because
// transport.SendStream implementations are expected to be safe for concurrent
// Write calls (mirroring quic-go semantics).
//
// Size buckets (power-of-two) give a coarse write-size histogram:
//
//	0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1K, 2K, 4K, 8K, 16K, 32K,
//	64K, 128K, 256K, 512K, 1M, 2M, 4M, 8M+
//
// Usage from tests:
//
//	cs := newCountingSendStream(realStream)
//	gw := newGroupWriter(cs, GroupSequence(0), nil)
//	err := gw.WriteFrame(frame)
//	cs.snapshot() // -> {writeCalls, bytesWritten, buckets}
type countingSendStream struct {
	inner transport.SendStream

	writeCalls  atomic.Int64
	bytesWitten atomic.Int64

	mu      sync.Mutex
	buckets []int64 // indexed by floor(log2(size+1))
}

// compile-time interface check
var _ transport.SendStream = (*countingSendStream)(nil)

// newCountingSendStream wraps the given SendStream with write instrumentation.
func newCountingSendStream(inner transport.SendStream) *countingSendStream {
	return &countingSendStream{inner: inner}
}

// bucketIndex returns the histogram bucket index for a write of n bytes.
// Bucket k covers sizes in (2^(k-1), 2^k]. size 0 -> bucket 0.
func bucketIndex(n int) int {
	if n <= 0 {
		return 0
	}
	k := 0
	v := 1
	for v < n {
		v <<= 1
		k++
	}
	// Guard against pathological growth beyond the slice; we grow lazily.
	return k
}

func (c *countingSendStream) growBucketsLocked(idx int) {
	for len(c.buckets) <= idx {
		c.buckets = append(c.buckets, 0)
	}
}

// Write counts the call and delegates to the wrapped stream. It does not
// alter the byte slice or split the write.
func (c *countingSendStream) Write(p []byte) (int, error) {
	n, err := c.inner.Write(p)
	c.writeCalls.Add(1)
	c.bytesWitten.Add(int64(n))

	idx := bucketIndex(n)
	c.mu.Lock()
	c.growBucketsLocked(idx)
	c.buckets[idx]++
	c.mu.Unlock()

	return n, err
}

// CancelWrite delegates to the wrapped stream.
func (c *countingSendStream) CancelWrite(code transport.StreamErrorCode) {
	c.inner.CancelWrite(code)
}

// SetWriteDeadline delegates to the wrapped stream.
func (c *countingSendStream) SetWriteDeadline(t time.Time) error {
	return c.inner.SetWriteDeadline(t)
}

// Close delegates to the wrapped stream.
func (c *countingSendStream) Close() error {
	return c.inner.Close()
}

// Context returns the wrapped stream's context.
func (c *countingSendStream) Context() context.Context {
	return c.inner.Context()
}

// SetPriority delegates to the wrapped stream.
func (c *countingSendStream) SetPriority(urgency int8, incremental bool) {
	c.inner.SetPriority(urgency, incremental)
}

// writeCounters is an immutable snapshot of the counting stream's state.
type writeCounters struct {
	writeCalls   int64   // total number of Write calls observed
	bytesWritten int64   // total bytes successfully written
	buckets      []int64 // write-size histogram (index = floor(log2(size)), size 0 -> 0)
}

// snapshot returns a consistent point-in-time view of the counters.
// The returned Buckets slice is a copy and may be freely mutated.
func (c *countingSendStream) snapshot() writeCounters {
	c.mu.Lock()
	buckets := make([]int64, len(c.buckets))
	copy(buckets, c.buckets)
	c.mu.Unlock()

	return writeCounters{
		writeCalls:   c.writeCalls.Load(),
		bytesWritten: c.bytesWitten.Load(),
		buckets:      buckets,
	}
}

// reset zeroes all counters. Useful between benchmark iterations.
func (c *countingSendStream) reset() {
	c.writeCalls.Store(0)
	c.bytesWitten.Store(0)
	c.mu.Lock()
	for i := range c.buckets {
		c.buckets[i] = 0
	}
	c.mu.Unlock()
}

// bytesWriterSink is an io.Writer backed by a growable *bytes.Buffer-compatible
// sink that performs a REAL memcpy on every Write (unlike io.Discard, which is
// a no-op). It is used as the destination for measurement when standing up a
// real QUIC server/client pair is impractical.
//
// The buffer grows without bound by design; benchmarks reset it per iteration
// or use it for short, bounded runs.
type bytesWriterSink struct {
	mu  sync.Mutex
	buf []byte
}

// newBytesWriterSink returns a sink with the given initial capacity.
func newBytesWriterSink(cap int) *bytesWriterSink {
	return &bytesWriterSink{buf: make([]byte, 0, cap)}
}

// Write performs an actual copy of p into the growable buffer and returns
// len(p), nil. The copy is the measured work.
func (s *bytesWriterSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.mu.Unlock()
	return len(p), nil
}

// reset clears the buffer while retaining its capacity.
func (s *bytesWriterSink) reset() {
	s.mu.Lock()
	s.buf = s.buf[:0]
	s.mu.Unlock()
}

// Compile-time check that bytesWriterSink is an io.Writer.
var _ io.Writer = (*bytesWriterSink)(nil)
