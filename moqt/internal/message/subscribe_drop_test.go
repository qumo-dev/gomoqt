package message

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeDropMessage_EncodeDecode(t *testing.T) {
	t.Run("valid_message", func(t *testing.T) {
		original := SubscribeDropMessage{
			StartGroup: 42,
			EndGroup:   43,
			ErrorCode:  3,
		}

		var buf bytes.Buffer
		err := original.Encode(&buf)
		require.NoError(t, err)

		var decoded SubscribeDropMessage
		err = decoded.Decode(&buf)
		require.NoError(t, err)

		assert.Equal(t, original.StartGroup, decoded.StartGroup)
		assert.Equal(t, original.EndGroup, decoded.EndGroup)
		assert.Equal(t, original.ErrorCode, decoded.ErrorCode)
	})

	t.Run("zero_values", func(t *testing.T) {
		original := SubscribeDropMessage{
			StartGroup: 0,
			EndGroup:   0,
			ErrorCode:  1,
		}

		var buf bytes.Buffer
		err := original.Encode(&buf)
		require.NoError(t, err)

		var decoded SubscribeDropMessage
		err = decoded.Decode(&buf)
		require.NoError(t, err)

		assert.Equal(t, uint64(0), decoded.StartGroup)
		assert.Equal(t, uint64(0), decoded.EndGroup)
		assert.Equal(t, uint64(1), decoded.ErrorCode)
	})

	t.Run("max_values", func(t *testing.T) {
		// Max varint value in MOQ (62-bit limit)
		maxVal := uint64(1<<62) - 1
		original := SubscribeDropMessage{
			StartGroup: maxVal,
			EndGroup:   maxVal,
			ErrorCode:  maxVal,
		}

		var buf bytes.Buffer
		err := original.Encode(&buf)
		require.NoError(t, err)

		var decoded SubscribeDropMessage
		err = decoded.Decode(&buf)
		require.NoError(t, err)

		assert.Equal(t, maxVal, decoded.StartGroup)
		assert.Equal(t, maxVal, decoded.EndGroup)
		assert.Equal(t, maxVal, decoded.ErrorCode)
	})
}

func TestSubscribeDropMessage_DecodeErrors(t *testing.T) {
	t.Run("read_message_length_error", func(t *testing.T) {
		var sdm SubscribeDropMessage
		var buf bytes.Buffer
		src := bytes.NewReader(buf.Bytes())
		err := sdm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read_full_error", func(t *testing.T) {
		var sdm SubscribeDropMessage
		var buf bytes.Buffer
		buf.WriteByte(0x10) // length varint = 16
		src := bytes.NewReader(buf.Bytes())
		err := sdm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read_varint_error_for_type", func(t *testing.T) {
		var sdm SubscribeDropMessage
		var buf bytes.Buffer
		buf.WriteByte(0x01) // length varint = 1
		buf.WriteByte(0x80) // invalid varint (incomplete)
		src := bytes.NewReader(buf.Bytes())
		err := sdm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("invalid_type", func(t *testing.T) {
		var sdm SubscribeDropMessage
		var buf bytes.Buffer
		buf.WriteByte(0x02) // length varint = 2
		buf.WriteByte(0x00) // invalid type
		buf.WriteByte(0x80) // invalid varint (incomplete)
		src := bytes.NewReader(buf.Bytes())
		err := sdm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra_data", func(t *testing.T) {
		var sdm SubscribeDropMessage
		var buf bytes.Buffer
		buf.WriteByte(0x05) // length varint = 5
		buf.WriteByte(0x01) // type = 1
		buf.WriteByte(0x00) // StartGroup = 0
		buf.WriteByte(0x01) // EndGroup = 1
		buf.WriteByte(0x02) // ErrorCode = 2
		buf.WriteByte(0xFF) // extra byte inside the message body
		src := bytes.NewReader(buf.Bytes())
		err := sdm.Decode(src)
		assert.Equal(t, ErrMessageTooShort, err)
	})
}
