package moqt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// announceWire encodes an Announce Stream response: ANNOUNCE_OK carrying
// okHop, then each announcement message in order.
func announceWire(tb testing.TB, okHop uint64, msgs ...interface{ Encode(io.Writer) error }) []byte {
	tb.Helper()
	var buf bytes.Buffer
	require.NoError(tb, message.AnnounceOkMessage{HopID: okHop}.Encode(&buf))
	for _, m := range msgs {
		require.NoError(tb, m.Encode(&buf))
	}
	return buf.Bytes()
}

// violationRecorder records the protocol violations a reader reports.
type violationRecorder struct {
	ch chan string
}

func newViolationRecorder() *violationRecorder {
	return &violationRecorder{ch: make(chan string, 4)}
}

func (v *violationRecorder) record(msg string) {
	select {
	case v.ch <- msg:
	default:
	}
}

// wait returns the first violation reported within timeout, or "".
func (v *violationRecorder) wait(timeout time.Duration) string {
	select {
	case msg := <-v.ch:
		return msg
	case <-time.After(timeout):
		return ""
	}
}

// receiveN receives n announcements, failing the test if they do not arrive.
func receiveN(t *testing.T, ar *AnnouncementReader, n int) []*Announcement {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	anns := make([]*Announcement, 0, n)
	for range n {
		ann, err := ar.ReceiveAnnouncement(ctx)
		require.NoError(t, err)
		anns = append(anns, ann)
	}
	return anns
}

func TestNewAnnouncementReader(t *testing.T) {
	mockStream := &fakeQUICStream{}
	prefix := "/test/prefix/"

	ras := newAnnouncementReader(mockStream, prefix, nil, announceReaderConfig{})

	require.NotNil(t, ras)
	assert.Equal(t, prefix, ras.prefix)
	assert.Equal(t, mockStream, ras.stream)
	assert.NotNil(t, ras.advertised)
	assert.Empty(t, ras.pendings)
	assert.NotNil(t, ras.announcedCh)
	assert.NotNil(t, ras.ctx)
}

func TestAnnouncementReader_ReceiveAnnouncement(t *testing.T) {
	tests := map[string]struct {
		reader      func() *AnnouncementReader
		ctx         context.Context
		wantErrType error
		wantPath    BroadcastPath
	}{
		"start is announced": {
			reader: func() *AnnouncementReader {
				data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "valid_announcement"})
				s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
				return newAnnouncementReader(s, "/test/", nil, announceReaderConfig{})
			},
			ctx:      context.Background(),
			wantPath: "/test/valid_announcement",
		},
		"context cancelled": {
			reader: func() *AnnouncementReader {
				s := &fakeQUICStream{reads: []streamResult{{Block: true}}}
				return newAnnouncementReader(s, "/test/", nil, announceReaderConfig{})
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			wantErrType: context.Canceled,
		},
		"reader closed": {
			reader: func() *AnnouncementReader {
				ras := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Block: true}}}, "/test/", nil, announceReaderConfig{})
				require.NoError(t, ras.Close())
				return ras
			},
			ctx:         context.Background(),
			wantErrType: context.Canceled,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ras := tt.reader()
			defer func() { _ = ras.Close() }() // not actionable: the test is over

			ctx, cancel := context.WithTimeout(tt.ctx, time.Second)
			defer cancel()
			ann, err := ras.ReceiveAnnouncement(ctx)

			if tt.wantErrType != nil {
				assert.ErrorIs(t, err, tt.wantErrType)
				assert.Nil(t, ann)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, ann.BroadcastPath())
			assert.True(t, ann.IsActive())
		})
	}
}

func TestAnnouncementReader_Close(t *testing.T) {
	ras := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Block: true}}}, "/test/", nil, announceReaderConfig{})

	assert.NoError(t, ras.Close())
	assert.NoError(t, ras.Close(), "a second Close is a no-op")
	assert.Error(t, ras.Context().Err())
}

func TestAnnouncementReader_CloseWithError(t *testing.T) {
	cancelled := make(chan transport.StreamErrorCode, 2)
	s := &fakeQUICStream{reads: []streamResult{{Block: true}}, cancelReadNotify: cancelled}
	ras := newAnnouncementReader(s, "/test/", nil, announceReaderConfig{})

	ras.CloseWithError(UninterestedErrorCode)
	ras.CloseWithError(AnnounceErrorCodeInternal) // a second call is harmless

	assert.Error(t, ras.Context().Err())
	select {
	case code := <-cancelled:
		assert.Equal(t, transport.StreamErrorCode(UninterestedErrorCode), code)
	case <-time.After(time.Second):
		t.Fatal("the stream was not reset")
	}
}

func TestAnnouncementReader_ImplicitAnnounceIDs(t *testing.T) {
	// Each ANNOUNCE_START takes the next id: 0, 1, 2. END 1 retires the
	// second, and a later START gets a fresh id 3, which END 3 then retires.
	data := announceWire(t, 0,
		message.AnnounceStartMessage{RoutePrefixSuffix: "a"},
		message.AnnounceStartMessage{RoutePrefixSuffix: "b"},
		message.AnnounceStartMessage{RoutePrefixSuffix: "c"},
		message.AnnounceEndMessage{AnnounceID: 1},
		message.AnnounceStartMessage{RoutePrefixSuffix: "b"},
		message.AnnounceEndMessage{AnnounceID: 3},
	)
	violations := newViolationRecorder()
	s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
	ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{onProtocolViolation: violations.record})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	anns := receiveN(t, ras, 4)
	byPath := make(map[BroadcastPath][]*Announcement)
	for _, ann := range anns {
		byPath[ann.BroadcastPath()] = append(byPath[ann.BroadcastPath()], ann)
	}

	require.Len(t, byPath["/live/b"], 2)
	for _, ann := range byPath["/live/b"] {
		select {
		case <-ann.Done():
		case <-time.After(time.Second):
			t.Fatal("both advertisements of b should have ended")
		}
	}
	assert.True(t, byPath["/live/a"][0].IsActive())
	assert.True(t, byPath["/live/c"][0].IsActive())
	assert.Empty(t, violations.wait(20*time.Millisecond))
}

func TestAnnouncementReader_UpdateInPlace(t *testing.T) {
	data := announceWire(t, 9,
		message.AnnounceStartMessage{RoutePrefixSuffix: "a", HopIDs: []uint64{1}, WarmRouteCost: 4, ColdRouteCost: 5},
		message.AnnounceUpdateMessage{AnnounceID: 0, HopIDs: []uint64{2, 3}, WarmRouteCost: 7, ColdRouteCost: 8},
	)
	s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
	ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{
		peerCost: func() uint64 { return 10 },
	})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	ann := receiveN(t, ras, 1)[0]

	// The update arrives after the start; wait for it to apply.
	assert.Eventually(t, func() bool { return len(ann.HopIDs()) == 3 }, time.Second, time.Millisecond)
	assert.Equal(t, []uint64{2, 3, 9}, ann.HopIDs(), "the peer's ANNOUNCE_OK Hop ID is the trailing entry")
	route := ann.routeInfo()
	assert.Equal(t, uint64(17), route.warmCost, "the peer's SETUP Cost is added")
	assert.Equal(t, uint64(18), route.coldCost)
	assert.True(t, ann.IsActive(), "an update does not end the announcement")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := ras.ReceiveAnnouncement(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "an update announces nothing new")
}

func TestAnnouncementReader_ZeroOKHopIDIsAppended(t *testing.T) {
	data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "a", HopIDs: []uint64{5}})
	s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
	ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	ann := receiveN(t, ras, 1)[0]
	assert.Equal(t, []uint64{5, 0}, ann.HopIDs(), "an unknown hop is kept as 0")
}

func TestAnnouncementReader_CostSaturates(t *testing.T) {
	data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "a", WarmRouteCost: maxRouteCost - 1, ColdRouteCost: maxRouteCost})
	s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
	ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{peerCost: func() uint64 { return 5 }})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	route := receiveN(t, ras, 1)[0].routeInfo()
	assert.Equal(t, uint64(maxRouteCost), route.warmCost)
	assert.Equal(t, uint64(maxRouteCost), route.coldCost)
}

func TestAnnouncementReader_LoopIsDiscarded(t *testing.T) {
	const self = 42
	data := announceWire(t, 7,
		// Loops through this endpoint: discarded, but its id is still live.
		message.AnnounceStartMessage{RoutePrefixSuffix: "looped", HopIDs: []uint64{self}},
		message.AnnounceStartMessage{RoutePrefixSuffix: "fine", HopIDs: []uint64{3}},
		// The looped route stops looping: it is announced now.
		message.AnnounceUpdateMessage{AnnounceID: 0, HopIDs: []uint64{4}},
		// The fine route starts looping: it ends.
		message.AnnounceUpdateMessage{AnnounceID: 1, HopIDs: []uint64{self}},
	)
	violations := newViolationRecorder()
	s := &fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}
	ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{
		localHopID:          self,
		onProtocolViolation: violations.record,
	})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	anns := receiveN(t, ras, 2)
	assert.Equal(t, BroadcastPath("/live/fine"), anns[0].BroadcastPath())
	assert.Equal(t, BroadcastPath("/live/looped"), anns[1].BroadcastPath())
	assert.Equal(t, []uint64{4, 7}, anns[1].HopIDs())

	select {
	case <-anns[0].Done():
	case <-time.After(time.Second):
		t.Fatal("a route that starts looping should end")
	}
	assert.True(t, anns[1].IsActive())
	assert.Empty(t, violations.wait(20*time.Millisecond))
}

func TestAnnouncementReader_ProtocolViolations(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"second start for an advertised route": {
			data: announceWire(t, 0,
				message.AnnounceStartMessage{RoutePrefixSuffix: "a"},
				message.AnnounceStartMessage{RoutePrefixSuffix: "a"},
			),
		},
		"end for an id never assigned": {
			data: announceWire(t, 0, message.AnnounceEndMessage{AnnounceID: 0}),
		},
		"end for a retired id": {
			data: announceWire(t, 0,
				message.AnnounceStartMessage{RoutePrefixSuffix: "a"},
				message.AnnounceEndMessage{AnnounceID: 0},
				message.AnnounceEndMessage{AnnounceID: 0},
			),
		},
		"update for an id never assigned": {
			data: announceWire(t, 0, message.AnnounceUpdateMessage{AnnounceID: 3}),
		},
		"repeated non-zero hop id": {
			data: announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "a", HopIDs: []uint64{5, 0, 5}}),
		},
		"unknown announcement type": {
			data: append(announceWire(t, 0), 0x07, 0x00),
		},
		"announcement before ANNOUNCE_OK": {
			data: func() []byte {
				var buf bytes.Buffer
				require.NoError(t, message.AnnounceStartMessage{RoutePrefixSuffix: "a"}.Encode(&buf))
				return buf.Bytes()
			}(),
		},
		"message cut short by FIN": {
			data: func() []byte {
				full := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "abcdef"})
				return full[:len(full)-3]
			}(),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			violations := newViolationRecorder()
			s := &fakeQUICStream{reads: []streamResult{{Data: tt.data}, {Err: io.EOF}}}
			ras := newAnnouncementReader(s, "/live/", nil, announceReaderConfig{onProtocolViolation: violations.record})
			defer func() { _ = ras.Close() }() // not actionable: the test is over

			assert.NotEmpty(t, violations.wait(time.Second), "the session should be closed")
			select {
			case <-ras.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("the reader should end")
			}
		})
	}
}

func TestAnnouncementReader_StreamEndIsNotAViolation(t *testing.T) {
	tests := map[string]struct {
		reads []streamResult
	}{
		"fin before ANNOUNCE_OK": {reads: []streamResult{{Err: io.EOF}}},
		"fin between messages": {
			reads: []streamResult{{Data: announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "a"})}, {Err: io.EOF}},
		},
		"stream reset mid-message": {
			reads: []streamResult{
				{Data: announceWire(t, 0)[:1]},
				{Err: &transport.StreamError{ErrorCode: 42, Remote: true}},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			violations := newViolationRecorder()
			ras := newAnnouncementReader(&fakeQUICStream{reads: tt.reads}, "/live/", nil, announceReaderConfig{onProtocolViolation: violations.record})
			defer func() { _ = ras.Close() }() // not actionable: the test is over

			select {
			case <-ras.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("the reader should end with the stream")
			}
			assert.Empty(t, violations.wait(20*time.Millisecond))
		})
	}
}

func TestAnnouncementReader_StreamEndEndsAnnouncements(t *testing.T) {
	data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "a"})
	ras := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Err: io.EOF}}}, "/live/", nil, announceReaderConfig{})

	ann := receiveN(t, ras, 1)[0]
	select {
	case <-ann.Done():
	case <-time.After(time.Second):
		t.Fatal("a closed stream should end every route on it")
	}
}

func TestAnnouncementReader_ConcurrentReceive(t *testing.T) {
	msgs := make([]interface{ Encode(io.Writer) error }, 0, 5)
	for i := range 5 {
		msgs = append(msgs, message.AnnounceStartMessage{RoutePrefixSuffix: fmt.Sprintf("stream%d", i)})
	}
	s := &fakeQUICStream{reads: []streamResult{{Data: announceWire(t, 0, msgs...)}, {Block: true}}}
	ras := newAnnouncementReader(s, "/test/", nil, announceReaderConfig{})
	defer func() { _ = ras.Close() }() // not actionable: the test is over

	var mu sync.Mutex
	got := make(map[BroadcastPath]bool)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ann, err := ras.ReceiveAnnouncement(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			got[ann.BroadcastPath()] = true
			mu.Unlock()
		})
	}
	wg.Wait()

	assert.Len(t, got, 5, "each announcement goes to exactly one receiver")
}

func TestAnnouncementReader_PrefixHandling(t *testing.T) {
	tests := map[string]struct {
		prefix       string
		suffix       string
		expectedPath BroadcastPath
	}{
		"simple":         {prefix: "/test/", suffix: "stream", expectedPath: "/test/stream"},
		"root prefix":    {prefix: "/", suffix: "stream", expectedPath: "/stream"},
		"nested suffix":  {prefix: "/test/", suffix: "a/b/stream", expectedPath: "/test/a/b/stream"},
		"clamped route":  {prefix: "/test/", suffix: "", expectedPath: "/test/"},
		"long prefix":    {prefix: "/very/long/nested/prefix/path/", suffix: "s", expectedPath: "/very/long/nested/prefix/path/s"},
		"leading slash":  {prefix: "/test/", suffix: "/stream", expectedPath: "/test//stream"},
		"nested prefix":  {prefix: "/test/sub/", suffix: "stream", expectedPath: "/test/sub/stream"},
		"dotted segment": {prefix: "/test/", suffix: "a.b", expectedPath: "/test/a.b"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: tt.suffix})
			ras := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}, tt.prefix, nil, announceReaderConfig{})
			defer func() { _ = ras.Close() }() // not actionable: the test is over

			assert.Equal(t, tt.expectedPath, receiveN(t, ras, 1)[0].BroadcastPath())
		})
	}
}

func TestNewAnnouncementReader_InvalidPrefixPanics(t *testing.T) {
	tests := map[string]struct {
		prefix string
	}{
		"empty":                {prefix: ""},
		"no trailing slash":    {prefix: "/test"},
		"no leading slash":     {prefix: "test/"},
		"bare segment no root": {prefix: "a"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() {
				newAnnouncementReader(&fakeQUICStream{}, tt.prefix, nil, announceReaderConfig{})
			})
		})
	}
}

func TestAnnouncementReader_StreamErrors(t *testing.T) {
	tests := map[string]struct {
		err error
	}{
		"quic stream error": {err: &transport.StreamError{StreamID: 123, ErrorCode: 42}},
		"generic io error":  {err: io.ErrUnexpectedEOF},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "valid_announcement"})
			ras := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Err: tt.err}}}, "/test/", nil, announceReaderConfig{})

			// The announcement read before the error is still delivered first.
			ann, err := ras.ReceiveAnnouncement(context.Background())
			require.NoError(t, err)
			assert.NotNil(t, ann)

			// Then the read error surfaces instead of blocking forever.
			ann2, err := ras.ReceiveAnnouncement(context.Background())
			assert.Nil(t, ann2)
			assert.ErrorIs(t, err, tt.err)
		})
	}
}

func TestAnnouncementReader_RouteOutsideScopeIsKeptLive(t *testing.T) {
	scope := newAuthScope(func() {})
	require.NoError(t, (&AuthWriter{scope: scope}).Grant(Grant{Root: "/room/1/", Publish: []string{"/"}}))

	data := announceWire(t, 0,
		message.AnnounceStartMessage{RoutePrefixSuffix: "2/bob"},   // id 0, outside the scope
		message.AnnounceStartMessage{RoutePrefixSuffix: "1/alice"}, // id 1
		message.AnnounceEndMessage{AnnounceID: 0},                  // still a live id
	)
	violations := newViolationRecorder()
	ar := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}, "/room/", scope,
		announceReaderConfig{onProtocolViolation: violations.record})
	defer func() { _ = ar.Close() }() // not actionable: the test is over

	assert.Equal(t, BroadcastPath("/room/1/alice"), receiveN(t, ar, 1)[0].BroadcastPath())
	assert.Empty(t, violations.wait(20*time.Millisecond))
}

// hopRange returns the Hop IDs from through to.
func hopRange(from, to uint64) []uint64 {
	ids := make([]uint64, 0, to-from+1)
	for id := from; id <= to; id++ {
		ids = append(ids, id)
	}
	return ids
}

func TestCheckHopIDs(t *testing.T) {
	tests := map[string]struct {
		ids     []uint64
		wantErr error
	}{
		"empty":                  {ids: nil},
		"distinct":               {ids: []uint64{1, 2, 3}},
		"repeated zeros":         {ids: []uint64{0, 4, 0, 0}},
		"repeated non-zero":      {ids: []uint64{5, 6, 5}, wantErr: errDuplicateHopID},
		"adjacent repeated hops": {ids: []uint64{0, 8, 8}, wantErr: errDuplicateHopID},
		"long distinct list":     {ids: hopRange(1, 1000)},
		"long list, zeros":       {ids: append(hopRange(1, 100), make([]uint64, 100)...)},
		"long list, last repeats first": {
			ids:     append(hopRange(1, 1000), 1),
			wantErr: errDuplicateHopID,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := checkHopIDs(tc.ids)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestAnnouncementReader_WiderScopeRestoresHiddenRoute(t *testing.T) {
	scope := newAuthScope(func() {})
	require.NoError(t, (&AuthWriter{scope: scope}).Grant(Grant{Root: "/room/1/", Publish: []string{"/"}}))
	data := announceWire(t, 0,
		message.AnnounceStartMessage{RoutePrefixSuffix: "2/bob"},   // outside the scope
		message.AnnounceStartMessage{RoutePrefixSuffix: "1/alice"}, // inside
	)
	ar := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}, "/room/", scope, announceReaderConfig{})
	defer func() { _ = ar.Close() }() // not actionable: the test is over
	require.Equal(t, BroadcastPath("/room/1/alice"), receiveN(t, ar, 1)[0].BroadcastPath())
	require.NoError(t, (&AuthWriter{scope: scope}).Grant(Grant{Root: "/room/2/", Publish: []string{"/"}}))

	ar.rescope()

	assert.Equal(t, BroadcastPath("/room/2/bob"), receiveN(t, ar, 1)[0].BroadcastPath())
}

func TestAnnouncementReader_RouteBeyondGrantServesOnlyItsPath(t *testing.T) {
	tests := map[string]struct {
		publish  []string
		wantDeep bool
	}{
		"grant covers the subtree": {publish: []string{"/"}, wantDeep: true},
		"grant is the exact path":  {publish: []string{"/alice"}, wantDeep: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scope := newAuthScope(func() {})
			require.NoError(t, (&AuthWriter{scope: scope}).Grant(Grant{Root: "/room/1/", Publish: tt.publish}))
			data := announceWire(t, 0, message.AnnounceStartMessage{RoutePrefixSuffix: "1/alice"})
			ar := newAnnouncementReader(&fakeQUICStream{reads: []streamResult{{Data: data}, {Block: true}}}, "/room/", scope, announceReaderConfig{})
			defer func() { _ = ar.Close() }() // not actionable: the test is over
			ann := receiveN(t, ar, 1)[0]
			// A relay re-serves the peer's announcement from its own mux.
			mux := NewTrackMux(0)
			mux.Announce(ann, TrackHandlerFunc(func(*TrackWriter) {}))

			exact, _ := mux.TrackHandler("/room/1/alice")
			deep, _ := mux.TrackHandler("/room/1/alice/cam1")

			assert.Same(t, ann, exact, "the announced path itself is always served")
			assert.Equal(t, tt.wantDeep, deep != nil)
		})
	}
}
