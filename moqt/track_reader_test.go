package moqt

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTrackReader(tb testing.TB) (*TrackReader, *FakeQUICStream) {
	tb.Helper()
	mockStream := &FakeQUICStream{}

	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})
	return receiver, mockStream
}

func TestNewTrackReader(t *testing.T) {
	mockStream := &FakeQUICStream{}
	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	assert.NotNil(t, receiver, "newTrackReader should not return nil")
	assert.Equal(t, BroadcastPath("/test"), receiver.BroadcastPath)
	assert.Equal(t, TrackName("video"), receiver.TrackName)
	assert.NotNil(t, receiver.queueing, "queue should be initialized")
	assert.NotNil(t, receiver.queuedCh, "queuedCh should be initialized")
	assert.NotNil(t, receiver.dequeued, "dequeued should be initialized")
}

func TestTrackReader_AcceptGroup(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Test with a timeout to ensure we don't block forever when no groups are available
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockStream := &FakeQUICStream{
		ParentCtx: ctx,
	}
	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	// Cancel the context
	cancel()

	// Test that AcceptGroup returns context error when context is cancelled
	testCtx, testCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer testCancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected error when context is cancelled")
	// Should return context.Canceled or DeadlineExceeded
	assert.True(t, err == context.Canceled || err == context.DeadlineExceeded, "expected context error")
}

func TestTrackReader_Context_FollowsStreamLifecycle(t *testing.T) {
	_, cancelSetup := context.WithCancel(context.Background())
	defer cancelSetup()

	mockStream := &FakeQUICStream{}

	substr := newTestSendSubscribeStreamFromStream(mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	// Cancel setup context; TrackReader context should remain alive while stream is alive.
	cancelSetup()
	select {
	case <-receiver.Context().Done():
		t.Fatal("track reader context should not be canceled by request setup context")
	case <-time.After(20 * time.Millisecond):
		// expected
	}

	// Close stream; TrackReader context should be canceled.
	require.NoError(t, mockStream.Close())

	select {
	case <-receiver.Context().Done():
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("track reader context should be canceled when stream is closed")
	}
}

func TestTrackReader_EnqueueGroup(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Mock receive stream
	mockReceiveStream := &FakeQUICReceiveStream{}

	// Enqueue a group
	receiver.enqueueGroup(GroupSequence(1), mockReceiveStream)

	// Test that we can accept the enqueued group
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	group, err := receiver.AcceptGroup(testCtx)
	assert.NoError(t, err, "should be able to accept enqueued group")
	assert.NotNil(t, group, "accepted group should not be nil")

}

func TestTrackReader_AcceptGroup_RealImplementation(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	// Test with a timeout to ensure we don't block forever
	testCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := receiver.AcceptGroup(testCtx)
	assert.Error(t, err, "expected timeout error when no groups are available")
	assert.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
}

func TestTrackReader_Close(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	err := receiver.Close()
	assert.NoError(t, err)

	// Close again should not error
	err = receiver.Close()
	assert.NoError(t, err)
}

// Close from another goroutine while AcceptGroup is blocked must be safe; run
// with -race. AcceptGroup then returns an error instead of blocking.
func TestTrackReader_CloseWhileAccepting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		receiver, _ := newTestTrackReader(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		accepted := make(chan error, 1)
		go func() {
			_, err := receiver.AcceptGroup(ctx)
			accepted <- err
		}()
		synctest.Wait() // AcceptGroup is blocked

		require.NoError(t, receiver.Close())

		assert.Error(t, <-accepted)
	})
}

func TestTrackReader_Update(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	newTrackConfig := SubscribeConfig{}

	_ = receiver.Update(&newTrackConfig)

	// Verify update
	assert.Equal(t, &SubscribeConfig{}, receiver.TrackConfig())
}

func TestTrackReader_AcceptDrop(t *testing.T) {
	var buf bytes.Buffer
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeDrop)})
	require.NoError(t, (message.SubscribeDropMessage{
		GroupStart: 11,
		GroupEnd:   21,
		ErrorCode:  3,
	}).Encode(&buf))

	mockStream := &FakeQUICStream{
		ReadFrom: &buf,
	}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	drop, err := receiver.acceptDrop(ctx)
	require.NoError(t, err)
	assert.Equal(t, SubscribeDrop{
		StartGroup: 11,
		EndGroup:   21,
		ErrorCode:  3,
	}, drop)
}

func TestTrackReader_CloseWithError(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	receiver.CloseWithError(SubscribeErrorCodeInternal)
}

func TestGroupReader_CancelRead_RemovesFromManager(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	recvStream := &FakeQUICReceiveStream{}
	group := newGroupReader(GroupSequence(1), recvStream, receiver.groupManager)

	assert.Len(t, receiver.groupManager.activeGroups, 1)
	assert.Contains(t, receiver.groupManager.activeGroups, group)

	group.CancelRead(InternalGroupErrorCode)
	assert.Len(t, receiver.groupManager.activeGroups, 0)
	assert.NotContains(t, receiver.groupManager.activeGroups, group)
}

func TestTrackReader_Drops(t *testing.T) {
	var buf bytes.Buffer

	// Write a SUBSCRIBE_DROP response (readSubscribeResponses returns after one drop)
	_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeDrop)})
	require.NoError(t, (message.SubscribeDropMessage{
		GroupStart: 11, // plain absolute sequence per draft-05
		GroupEnd:   21,
		ErrorCode:  3,
	}).Encode(&buf))

	mockStream := &FakeQUICStream{
		ReadFrom: &buf,
	}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var drops []SubscribeDrop
	for drop := range receiver.Drops(ctx) {
		drops = append(drops, drop)
	}

	require.Len(t, drops, 1)
	assert.Equal(t, GroupSequence(11), drops[0].StartGroup)
	assert.Equal(t, GroupSequence(21), drops[0].EndGroup)
	assert.Equal(t, SubscribeErrorCode(3), drops[0].ErrorCode)
}

func TestTrackReader_Drops_ContextCanceled(t *testing.T) {
	mockStream := &FakeQUICStream{}

	substr := newSendSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	go substr.readSubscribeResponses()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var drops []SubscribeDrop
	for drop := range receiver.Drops(ctx) {
		drops = append(drops, drop)
	}

	assert.Empty(t, drops)
}

func TestTrackReader_Update_NilConfig(t *testing.T) {
	receiver, _ := newTestTrackReader(t)

	err := receiver.Update(nil)
	assert.Error(t, err)
}

func TestTrackReader_SubscribeID(t *testing.T) {
	mockStream := &FakeQUICStream{}

	substr := newSendSubscribeStream(SubscribeID(42), mockStream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})

	assert.Equal(t, SubscribeID(42), receiver.SubscribeID())
}

// After SUBSCRIBE_END, AcceptGroup hands out every group the subscription is
// owed, from its start to the last one the END names, in whatever order they
// arrive, then returns io.EOF instead of blocking forever. A gap that never
// fills ends once the publisher has closed the subscribe stream and the grace
// has passed.
func TestTrackReader_AcceptGroup_EndsAfterSubscribeEnd(t *testing.T) {
	tests := map[string]struct {
		queued   []GroupSequence // queued before SUBSCRIBE_END
		late     []GroupSequence // queued after it
		end      GroupSequence
		closed   bool            // the publisher closes the subscribe stream after END
		afterFIN []GroupSequence // queued after the close, within the grace
		dropped  *SubscribeDrop  // SUBSCRIBE_DROP received after END
		noOK     bool            // END came without SUBSCRIBE_OK
		want     int             // groups returned before io.EOF
	}{
		"no groups":                                {end: 0, want: 0},
		"END without SUBSCRIBE_OK":                 {noOK: true, end: 2, want: 0},
		"last group already taken":                 {queued: []GroupSequence{1, 2}, end: 2, want: 2},
		"last group arrives after END":             {queued: []GroupSequence{1}, late: []GroupSequence{2}, end: 2, want: 2},
		"earlier group still in flight":            {queued: []GroupSequence{2}, late: []GroupSequence{1}, end: 2, want: 2},
		"out of order, all before END":             {queued: []GroupSequence{3, 1, 2}, end: 3, want: 3},
		"gap closed by SUBSCRIBE_DROP":             {queued: []GroupSequence{1, 4}, end: 4, dropped: &SubscribeDrop{StartGroup: 2, EndGroup: 3}, want: 2},
		"named group never arrives, stream closed": {queued: []GroupSequence{1}, end: 2, closed: true, want: 1},
		"named group arrives just after the close": {queued: []GroupSequence{1}, end: 2, closed: true, afterFIN: []GroupSequence{2}, want: 2},
	}
	for name, tt := range tests {
		// t.Run outside the bubble: synctest does not support t.Run inside it.
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// The subscribe stream stays open until gate is closed.
				gate := make(chan struct{})
				stream := &FakeQUICStream{Reads: []streamResult{{Block: true}}, ReadGate: gate}
				substr := newTestSendSubscribeStreamFromStream(stream, &SubscribeConfig{})
				receiver := newTrackReader("/test", "video", substr, func() {})
				if !tt.noOK {
					substr.setResolvedStart(1) // SUBSCRIBE_OK: the subscription starts at group 1
				}
				for _, seq := range tt.queued {
					receiver.enqueueGroup(seq, &FakeQUICReceiveStream{})
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				type result struct {
					groups int
					err    error
				}
				done := make(chan result, 1)
				go func() {
					var r result
					for {
						if _, err := receiver.AcceptGroup(ctx); err != nil {
							r.err = err
							done <- r
							return
						}
						r.groups++
					}
				}()
				synctest.Wait() // AcceptGroup has taken what was queued and blocks

				substr.setEnd(tt.end)
				for _, seq := range tt.late {
					receiver.enqueueGroup(seq, &FakeQUICReceiveStream{})
				}
				if tt.dropped != nil {
					substr.appendDrop(*tt.dropped)
				}
				if tt.closed {
					close(gate)
					synctest.Wait() // the close is seen before any group after it
					for _, seq := range tt.afterFIN {
						receiver.enqueueGroup(seq, &FakeQUICReceiveStream{})
					}
				} else {
					defer close(gate)
				}

				r := <-done
				assert.ErrorIs(t, r.err, io.EOF, "the track ends instead of blocking until the deadline")
				assert.Equal(t, tt.want, r.groups)
			})
		})
	}
}

// A second SUBSCRIBE_END is ignored: the first one names the last group.
func TestSendSubscribeStream_SetEnd_FirstWins(t *testing.T) {
	substr := newSendSubscribeStream(SubscribeID(1), &FakeQUICStream{}, &SubscribeConfig{})

	substr.setEnd(5)
	substr.setEnd(9)

	end, ended := substr.end()
	assert.True(t, ended)
	assert.Equal(t, GroupSequence(5), end)
}

// With a group still owed and the publisher's subscribe stream closed,
// AcceptGroup returns io.EOF exactly when the grace has passed, not before.
func TestTrackReader_AcceptGroup_GraceAfterClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &FakeQUICStream{} // reads EOF at once: the publisher closed the stream
		substr := newTestSendSubscribeStreamFromStream(stream, &SubscribeConfig{})
		receiver := newTrackReader("/test", "video", substr, func() {})
		substr.setResolvedStart(1)
		synctest.Wait() // readSubscribeResponses has seen the close
		substr.setEnd(3) // groups 1 to 3 are owed; none will come
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		start := time.Now()

		_, err := receiver.AcceptGroup(ctx)

		assert.ErrorIs(t, err, io.EOF)
		assert.Equal(t, subscribeEndGrace, time.Since(start), "waits the whole grace, no longer")
	})
}

// io.EOF is sticky, and a group that arrives after it is cancelled, not
// queued where no reader will take it.
func TestTrackReader_AcceptGroup_EOFIsSticky(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	stream := &FakeQUICStream{Reads: []streamResult{{Block: true}}, ReadGate: gate}
	substr := newTestSendSubscribeStreamFromStream(stream, &SubscribeConfig{})
	receiver := newTrackReader("/test", "video", substr, func() {})
	substr.setResolvedStart(1)
	receiver.enqueueGroup(1, &FakeQUICReceiveStream{})
	substr.setEnd(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := receiver.AcceptGroup(ctx)
	require.NoError(t, err)
	_, err = receiver.AcceptGroup(ctx)
	require.ErrorIs(t, err, io.EOF)
	late := &FakeQUICReceiveStream{}

	receiver.enqueueGroup(2, late)

	_, err = receiver.AcceptGroup(ctx)
	assert.ErrorIs(t, err, io.EOF, "EOF again, not the late group")
	assert.NotEmpty(t, late.CancelReadCodes(), "the late group's stream is released")
}

func TestSettledGroups_Add(t *testing.T) {
	tests := map[string]struct {
		add  [][2]GroupSequence
		want []groupRange
	}{
		"in order is one range":        {add: [][2]GroupSequence{{1, 1}, {2, 2}, {3, 3}}, want: []groupRange{{1, 3}}},
		"out of order merges":          {add: [][2]GroupSequence{{3, 3}, {1, 1}, {2, 2}}, want: []groupRange{{1, 3}}},
		"a gap stays a gap":            {add: [][2]GroupSequence{{1, 1}, {3, 3}}, want: []groupRange{{1, 1}, {3, 3}}},
		"a dropped range fills it":     {add: [][2]GroupSequence{{1, 1}, {4, 4}, {2, 3}}, want: []groupRange{{1, 4}}},
		"overlapping ranges merge":     {add: [][2]GroupSequence{{1, 5}, {3, 8}}, want: []groupRange{{1, 8}}},
		"a range inside another":       {add: [][2]GroupSequence{{1, 9}, {3, 4}}, want: []groupRange{{1, 9}}},
		"an inverted range is ignored": {add: [][2]GroupSequence{{5, 4}}, want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var s settledGroups

			for _, r := range tt.add {
				s.add(r[0], r[1])
			}

			assert.Equal(t, tt.want, s.ranges)
		})
	}
}

// A publisher that skips every other group without SUBSCRIBE_DROP leaves a gap
// per group; past the cap, the oldest gap is given up.
func TestSettledGroups_Add_Bounded(t *testing.T) {
	var s settledGroups

	for i := range GroupSequence(maxSettledRanges + 10) {
		s.add(2*i+1, 2*i+1)
	}

	assert.Len(t, s.ranges, maxSettledRanges)
}

func TestSettledGroups_Covers(t *testing.T) {
	settled := settledGroups{ranges: []groupRange{{1, 3}, {6, 9}}}
	tests := map[string]struct {
		lo, hi GroupSequence
		want   bool
	}{
		"inside one range":     {lo: 1, hi: 3, want: true},
		"a single group":       {lo: 7, hi: 7, want: true},
		"across a gap":         {lo: 1, hi: 6, want: false},
		"inside the gap":       {lo: 4, hi: 5, want: false},
		"past the last range":  {lo: 9, hi: 10, want: false},
		"before the first one": {lo: 0, hi: 1, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, settled.covers(tt.lo, tt.hi))
		})
	}
	t.Run("empty", func(t *testing.T) {
		var empty settledGroups
		assert.False(t, empty.covers(1, 1))
	})
}
