package moqt

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agedGroup opens a group of seq tracked by ages, on a fake stream.
func agedGroup(tb testing.TB, ages *groupAges, seq GroupSequence) (*GroupWriter, *fakeQUICSendStream) {
	tb.Helper()
	stream := &fakeQUICSendStream{}
	gw := newGroupWriter(stream, seq, nil)
	gw.ages = ages
	ages.add(gw)
	return gw, stream
}

// writeAt writes an empty frame at timestamp ts.
func writeAt(tb testing.TB, gw *GroupWriter, ts uint64) {
	tb.Helper()
	f := NewFrame(0)
	f.Timestamp = ts
	require.NoError(tb, gw.WriteFrame(f))
}

func TestGroupAges_Expiry(t *testing.T) {
	// Timescale 1000: timestamps are milliseconds. Group 1 starts at 0, and
	// group 2's first frame at 100 is group 1's reach.
	tests := map[string]struct {
		maxAge      uint64
		g2Frames    []uint64
		wantExpired bool
	}{
		"age below max age":              {maxAge: 500, g2Frames: []uint64{100, 599}},
		"age equal to max age expires":   {maxAge: 500, g2Frames: []uint64{100, 600}, wantExpired: true},
		"age above max age expires":      {maxAge: 500, g2Frames: []uint64{100, 900}, wantExpired: true},
		"zero max age keeps only latest": {maxAge: 0, g2Frames: []uint64{100}, wantExpired: true},
		"timestamps going backwards":     {maxAge: 500, g2Frames: []uint64{100, 50}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config := &SubscribeConfig{MaxAge: tt.maxAge}
			ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 1000}

			g1, s1 := agedGroup(t, ages, 1)
			writeAt(t, g1, 0)
			writeAt(t, g1, 450) // its own later frames do not bound it

			g2, s2 := agedGroup(t, ages, 2)
			for _, ts := range tt.g2Frames {
				writeAt(t, g2, ts)
			}

			if tt.wantExpired {
				assert.Equal(t, []transport.StreamErrorCode{streamDeliveryTimeout}, s1.CancelWriteCodes())
			} else {
				assert.Empty(t, s1.CancelWriteCodes())
			}
			assert.Empty(t, s2.CancelWriteCodes(), "the latest group never expires")
		})
	}
}

func TestGroupAges_SuccessorWithoutFrameNeverExpires(t *testing.T) {
	config := &SubscribeConfig{MaxAge: 0}
	ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 1000}

	g1, s1 := agedGroup(t, ages, 1)
	writeAt(t, g1, 0)
	_, _ = agedGroup(t, ages, 2) // opened, but has presented no frame
	writeAt(t, g1, 10_000)

	assert.Empty(t, s1.CancelWriteCodes(), "nothing proves where group 1 stops")
}

func TestGroupAges_ReachIsTheNextGroupBySequence(t *testing.T) {
	// Groups 1 and 3 are open; group 2 already closed. Group 1's reach is
	// group 2's first frame, not group 3's.
	config := &SubscribeConfig{MaxAge: 1000}
	ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 1000}

	g1, s1 := agedGroup(t, ages, 1)
	writeAt(t, g1, 0)
	g2, _ := agedGroup(t, ages, 2)
	writeAt(t, g2, 200)
	require.NoError(t, g2.Close())
	g3, s3 := agedGroup(t, ages, 3)
	writeAt(t, g3, 1100) // age of group 1 = 1100-200 = 900 < 1000
	assert.Empty(t, s1.CancelWriteCodes())

	writeAt(t, g3, 1200) // age of group 1 = 1000: expired
	assert.Equal(t, []transport.StreamErrorCode{streamDeliveryTimeout}, s1.CancelWriteCodes())
	assert.Empty(t, s3.CancelWriteCodes(), "the latest group never expires")
}

func TestGroupAges_Timescale(t *testing.T) {
	// 90 kHz: 45000 units are 500 ms.
	config := &SubscribeConfig{MaxAge: 500}
	ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 90_000}

	g1, s1 := agedGroup(t, ages, 1)
	writeAt(t, g1, 0)
	g2, _ := agedGroup(t, ages, 2)
	writeAt(t, g2, 1000)
	writeAt(t, g2, 1000+44_999)
	assert.Empty(t, s1.CancelWriteCodes())

	writeAt(t, g2, 1000+45_000)
	assert.Len(t, s1.CancelWriteCodes(), 1)
}

func TestGroupAges_HugeMaxAgeDoesNotOverflow(t *testing.T) {
	config := &SubscribeConfig{MaxAge: 1<<62 - 1}
	ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 90_000}

	g1, s1 := agedGroup(t, ages, 1)
	writeAt(t, g1, 0)
	g2, _ := agedGroup(t, ages, 2)
	writeAt(t, g2, 1)
	writeAt(t, g2, 1<<61)

	assert.Empty(t, s1.CancelWriteCodes())
}

func TestGroupAges_PrunesClosedGroups(t *testing.T) {
	config := &SubscribeConfig{}
	ages := &groupAges{config: func() *SubscribeConfig { return config }, timescale: 1000}

	for seq := GroupSequence(1); seq <= 100; seq++ {
		gw, _ := agedGroup(t, ages, seq)
		writeAt(t, gw, uint64(seq))
		require.NoError(t, gw.Close())
	}

	ages.mu.Lock()
	defer ages.mu.Unlock()
	assert.Len(t, ages.groups, 1, "sequential groups keep only the newest")
}

func TestGroupWriter_FrameRange(t *testing.T) {
	tests := map[string]struct {
		frameStart, frameEnd uint64
		wantTimestamps       []uint64
	}{
		"whole group":        {wantTimestamps: []uint64{10, 20, 30, 40}},
		"from frame 2":       {frameStart: 2, wantTimestamps: []uint64{30, 40}},
		"before frame 2":     {frameEnd: 2, wantTimestamps: []uint64{10, 20}},
		"frames 1 and 2":     {frameStart: 1, frameEnd: 3, wantTimestamps: []uint64{20, 30}},
		"start past the end": {frameStart: 9, wantTimestamps: nil},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			stream := &fakeQUICSendStream{}
			gw := newGroupWriter(stream, 1, nil)
			gw.frameStart, gw.frameEnd = tt.frameStart, tt.frameEnd

			for _, ts := range []uint64{10, 20, 30, 40} {
				writeAt(t, gw, ts)
			}

			// The first frame sent is delta-encoded from 0, so the stream
			// decodes on its own.
			r := bytes.NewReader(stream.Written())
			var got []uint64
			prev := uint64(0)
			for r.Len() > 0 {
				f := NewFrame(0)
				require.NoError(t, f.decode(r, prev))
				prev = f.Timestamp
				got = append(got, f.Timestamp)
			}
			assert.Equal(t, tt.wantTimestamps, got)
		})
	}
}
