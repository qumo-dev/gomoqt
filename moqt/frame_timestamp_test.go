package moqt

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupWriterReader_TimestampRoundTrip(t *testing.T) {
	tests := map[string]struct {
		timestamps []uint64
	}{
		"increasing timestamps": {timestamps: []uint64{1000, 2000, 3000}},
		"decreasing timestamps": {timestamps: []uint64{3000, 2000, 1000}},
		"equal timestamps":      {timestamps: []uint64{500, 500, 500}},
		"zero first timestamp":  {timestamps: []uint64{0, 33, 66}},
		"large first timestamp": {timestamps: []uint64{1 << 50, 1<<50 + 3000}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			writer := &GroupWriter{stream: &FakeQUICSendStream{WriteFunc: buf.Write}}
			for i, ts := range tt.timestamps {
				frame := NewFrame(8)
				frame.Timestamp = ts
				_, _ = frame.Write([]byte{byte(i)})
				require.NoError(t, writer.WriteFrame(frame))
			}

			reader := &GroupReader{stream: &FakeQUICReceiveStream{ReadFunc: buf.Read}}
			got := NewFrame(8)
			for i, want := range tt.timestamps {
				require.NoError(t, reader.ReadFrame(got))
				assert.Equal(t, want, got.Timestamp, "frame %d timestamp", i)
				assert.Equal(t, []byte{byte(i)}, got.Body(), "frame %d payload", i)
			}
		})
	}
}

func TestFrame_Clone_CopiesTimestamp(t *testing.T) {
	frame := NewFrame(4)
	frame.Timestamp = 90000
	_, _ = frame.Write([]byte("abc"))

	clone := frame.Clone()

	assert.Equal(t, frame.Timestamp, clone.Timestamp)
	assert.Equal(t, frame.Body(), clone.Body())
}

func TestFrame_Encode_RejectsOutOfRangeTimestamp(t *testing.T) {
	// A timestamp whose delta from prevTimestamp exceeds the 62-bit varint
	// range (reachable on a relay via uint64->int64 wraparound) must return
	// ErrTimestampOutOfRange instead of panicking inside WriteVarint.
	tests := map[string]struct {
		ts        uint64
	}{
		"large positive delta": {ts: uint64(1<<63 - 1)},
		"wraparound negative":  {ts: 1}, // prev=1<<63-1 makes delta hugely negative
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := NewFrame(0)
			f.Timestamp = tt.ts
			var buf bytes.Buffer

			prev := uint64(0)
			if name == "wraparound negative" {
				prev = uint64(1<<63 - 1)
			}
			err := f.encode(&buf, prev)
			assert.ErrorIs(t, err, ErrTimestampOutOfRange)
		})
	}
}
