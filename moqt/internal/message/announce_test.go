package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnounceMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.AnnounceMessage
		wantErr bool
	}{
		"valid message": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.AnnounceStatus(1),
				BroadcastPathSuffix: "path/to/track",
				HopIDs:              []uint64{},
			},
		},
		"empty wildcard parameters": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.AnnounceStatus(1),
				BroadcastPathSuffix: "",
				HopIDs:              []uint64{},
			},
		},
		"max values": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.AnnounceStatus(^byte(0)),
				BroadcastPathSuffix: "very/long/path",
				HopIDs:              []uint64{1, 2, 3},
			},
		},
		"with hop ids": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.ACTIVE,
				BroadcastPathSuffix: "test",
				HopIDs:              []uint64{100, 200},
			},
		},
		"with hop ids and path cost": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.ACTIVE,
				BroadcastPathSuffix: "test",
				HopIDs:              []uint64{100, 200},
				PathCost:            250_000,
			},
		},
		"zero path cost decodes as zero": {
			input: message.AnnounceMessage{
				AnnounceStatus:      message.ACTIVE,
				BroadcastPathSuffix: "test",
				HopIDs:              []uint64{1},
				PathCost:            0,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			// Encode
			err := tc.input.Encode(&buf)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Decode
			var decoded message.AnnounceMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestAnnounceMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var am message.AnnounceMessage
		src := bytes.NewReader([]byte{}) // empty, should cause error
		err := am.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var am message.AnnounceMessage
		// Write length but not enough data
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10) // varint for 10
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2]) // only 2 bytes, but length says 10
		err := am.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error", func(t *testing.T) {
		var am message.AnnounceMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1) // length 1
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := am.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read string error", func(t *testing.T) {
		var am message.AnnounceMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 2) // length 2
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // status
		buf.WriteByte(0x80) // invalid string varint
		src := bytes.NewReader(buf.Bytes())
		err := am.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra data", func(t *testing.T) {
		var am message.AnnounceMessage
		// Manually construct data with extra bytes after valid data. A
		// single trailing varint is the optional PathCost field, so the
		// garbage must extend past one varint to be rejected.
		var buf bytes.Buffer
		buf.WriteByte(0x06) // length varint = 6
		buf.WriteByte(0x01) // status
		buf.WriteByte(0x01) // string length 1
		buf.WriteByte('a')  // string
		buf.WriteByte(0x00) // hops
		buf.WriteByte(0x00) // PathCost varint (consumed)
		buf.WriteByte(0x00) // extra byte beyond it
		src := bytes.NewReader(buf.Bytes())
		err := am.Decode(src)
		assert.Error(t, err)
		assert.Equal(t, message.ErrMessageTooShort, err)
	})
}

// TestAnnounceMessage_ZeroCostOmission verifies the wire-compatibility
// contract: a zero PathCost produces byte-identical output to a message
// without the field, so cost-unaware senders keep emitting the previous
// format and older peers decode it unchanged.
func TestAnnounceMessage_ZeroCostOmission(t *testing.T) {
	withCost := message.AnnounceMessage{
		AnnounceStatus:      message.ACTIVE,
		BroadcastPathSuffix: "test",
		HopIDs:              []uint64{7, 8},
	}
	withoutCost := withCost

	var a, b bytes.Buffer
	require.NoError(t, withoutCost.Encode(&a))

	withCost.PathCost = 1 // any nonzero changes the bytes
	require.NoError(t, withCost.Encode(&b))
	assert.NotEqual(t, a.Bytes(), b.Bytes(), "nonzero cost must be present on the wire")

	withCost.PathCost = 0
	var c bytes.Buffer
	require.NoError(t, withCost.Encode(&c))
	assert.Equal(t, a.Bytes(), c.Bytes(), "zero cost must encode identically to an omitted field")
}
