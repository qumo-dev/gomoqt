package moqt

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestOpenGroup_BackpressuresOnStreamLimit is the regression test for the
// stream-churn stall: a publisher that opens one group per frame — i.e. one new
// unidirectional QUIC stream per frame — faster than the peer recycles streams.
//
// Before the fix, TrackWriter.OpenGroup propagated quic-go's non-blocking
// StreamLimitReachedError ("too many open streams") once the peer's MAX_STREAMS
// credit (~100 concurrent uni streams) was exhausted, and a publisher that
// treated that error as fatal aborted the whole track. After that nothing
// flowed, and quic-go's idle timeout killed the connection (~30s "no recent
// network activity"). OpenGroup now backpressures via OpenUniStreamSync, so
// groups keep flowing past the stream limit at the subscriber's drain rate.
//
// This reads far more groups than the ~100 concurrent-stream limit to prove the
// connection survives. With the fix it completes in well under a second; a
// regression would hang until the context timeout and fail.
func TestOpenGroup_BackpressuresOnStreamLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("real-QUIC integration test; skipped in -short")
	}
	// Bound the run: completes fast with the fix; a regression otherwise hangs
	// ~30s on the idle timeout before this fires.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1K frames, one group per frame -> maximum uni-stream churn.
	// Gate the publisher's flood until the subscribe handshake completes: the
	// unpaced uni-stream churn otherwise races the SUBSCRIBE_OK read on slow/CI
	// runners (the client sees EOF). Closed after dialAndSubscribe below.
	start := make(chan struct{})
	srv, addr := setupSaturatingServer(t, ctx, 1<<10, 1, start)
	defer closeBroadcastServer(t, srv)

	sess, tr := dialAndSubscribe(t, ctx, addr)
	defer sess.CloseWithError(NoError, "done")
	defer tr.Close()
	close(start)

	// quic-go's default concurrent uni-stream limit is ~100; read well past it.
	const minGroups = 500
	frame := NewFrame(1 << 10)
	got := 0
	for got < minGroups {
		gr, err := tr.AcceptGroup(ctx)
		if err != nil {
			t.Fatalf("stopped after %d groups — regression: OpenGroup no longer backpressures on stream limit: %v", got, err)
		}
		for {
			if err := gr.ReadFrame(frame); err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("read frame after %d groups: %v", got, err)
			}
			got++
			if got >= minGroups {
				break
			}
		}
	}

	assert.GreaterOrEqual(t, got, minGroups, "should read past the stream limit via backpressure")
}
