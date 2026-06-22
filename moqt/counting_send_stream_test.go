package moqt

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

// CountingSendStream wraps a transport.SendStream to instrument Write calls
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
//	cs := NewCountingSendStream(realStream)
//	gw := newGroupWriter(cs, GroupSequence(0), nil)
//	_ = gw.WriteFrame(frame)
//	cs.Snapshot() // -> {WriteCalls, BytesWritten, Buckets}
type CountingSendStream struct {
	inner transport.SendStream

	writeCalls  atomic.Int64
	bytesWitten atomic.Int64

	mu      sync.Mutex
	buckets []int64 // indexed by floor(log2(size+1))
}

// compile-time interface check
var _ transport.SendStream = (*CountingSendStream)(nil)

// NewCountingSendStream wraps the given SendStream with write instrumentation.
func NewCountingSendStream(inner transport.SendStream) *CountingSendStream {
	return &CountingSendStream{inner: inner}
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

func (c *CountingSendStream) growBucketsLocked(idx int) {
	for len(c.buckets) <= idx {
		c.buckets = append(c.buckets, 0)
	}
}

// Write counts the call and delegates to the wrapped stream. It does not
// alter the byte slice or split the write.
func (c *CountingSendStream) Write(p []byte) (int, error) {
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
func (c *CountingSendStream) CancelWrite(code transport.StreamErrorCode) {
	c.inner.CancelWrite(code)
}

// SetWriteDeadline delegates to the wrapped stream.
func (c *CountingSendStream) SetWriteDeadline(t time.Time) error {
	return c.inner.SetWriteDeadline(t)
}

// Close delegates to the wrapped stream.
func (c *CountingSendStream) Close() error {
	return c.inner.Close()
}

// Context returns the wrapped stream's context.
func (c *CountingSendStream) Context() context.Context {
	return c.inner.Context()
}

// Inner returns the wrapped SendStream.
func (c *CountingSendStream) Inner() transport.SendStream { return c.inner }

// WriteCounters is an immutable snapshot of the counting stream's state.
type WriteCounters struct {
	WriteCalls   int64   // total number of Write calls observed
	BytesWritten int64   // total bytes successfully written
	Buckets      []int64 // write-size histogram (index = floor(log2(size)), size 0 -> 0)
}

// Snapshot returns a consistent point-in-time view of the counters.
// The returned Buckets slice is a copy and may be freely mutated.
func (c *CountingSendStream) Snapshot() WriteCounters {
	c.mu.Lock()
	buckets := make([]int64, len(c.buckets))
	copy(buckets, c.buckets)
	c.mu.Unlock()

	return WriteCounters{
		WriteCalls:   c.writeCalls.Load(),
		BytesWritten: c.bytesWitten.Load(),
		Buckets:      buckets,
	}
}

// Reset zeroes all counters. Useful between benchmark iterations.
func (c *CountingSendStream) Reset() {
	c.writeCalls.Store(0)
	c.bytesWitten.Store(0)
	c.mu.Lock()
	for i := range c.buckets {
		c.buckets[i] = 0
	}
	c.mu.Unlock()
}

// BucketLabel returns a human-readable label for the given bucket index
// (e.g. "64", "1K", "64K"), matching the power-of-two boundaries used by
// bucketIndex.
func BucketLabel(idx int) string {
	if idx <= 0 {
		return "0"
	}
	v := 1 << idx
	switch {
	case v >= 1<<20:
		return formatBytesLabel(v, 1<<20, "M")
	case v >= 1<<10:
		return formatBytesLabel(v, 1<<10, "K")
	default:
		return itoa(v)
	}
}

func formatBytesLabel(v, unit int, suffix string) string {
	if v%unit == 0 {
		return itoa(v/unit) + suffix
	}
	return itoa(v) // fall back to raw bytes for non-round sizes
}

// itoa is a small, allocation-free int->string helper to avoid pulling in fmt
// in hot reporting paths.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// BytesWriterSink is an io.Writer backed by a growable *bytes.Buffer-compatible
// sink that performs a REAL memcpy on every Write (unlike io.Discard, which is
// a no-op). It is used as the destination for measurement when standing up a
// real QUIC server/client pair is impractical.
//
// The buffer grows without bound by design; benchmarks reset it per iteration
// or use it for short, bounded runs.
type BytesWriterSink struct {
	mu  sync.Mutex
	buf []byte
}

// NewBytesWriterSink returns a sink with the given initial capacity.
func NewBytesWriterSink(cap int) *BytesWriterSink {
	return &BytesWriterSink{buf: make([]byte, 0, cap)}
}

// Write performs an actual copy of p into the growable buffer and returns
// len(p), nil. The copy is the measured work.
func (s *BytesWriterSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	s.mu.Unlock()
	return len(p), nil
}

// Len returns the current number of buffered bytes.
func (s *BytesWriterSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

// Reset clears the buffer while retaining its capacity.
func (s *BytesWriterSink) Reset() {
	s.mu.Lock()
	s.buf = s.buf[:0]
	s.mu.Unlock()
}

// Compile-time check that BytesWriterSink is an io.Writer.
var _ io.Writer = (*BytesWriterSink)(nil)
