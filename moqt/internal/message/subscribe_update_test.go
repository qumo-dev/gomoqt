package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeUpdateMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.SubscribeUpdateMessage
		wantErr bool
	}{
		"valid message": {
			input: message.SubscribeUpdateMessage{
				SubscriberPriority: 5,
			},
		},
		"zero priority": {
			input: message.SubscribeUpdateMessage{
				SubscriberPriority: 0,
			},
		},
		"max priority": {
			input: message.SubscribeUpdateMessage{
				SubscriberPriority: 255,
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
			var decoded message.SubscribeUpdateMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare all fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestSubscribeUpdateMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		src := bytes.NewReader([]byte{})
		err := sum.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := sum.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for track priority", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1)
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := sum.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for min group sequence", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 2)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := sum.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for max group sequence", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 3)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x01) // min group sequence
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := sum.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra data", func(t *testing.T) {
		var sum message.SubscribeUpdateMessage
		var buf bytes.Buffer
		buf.WriteByte(0x04) // length varint = 4
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x01) // min group sequence
		buf.WriteByte(0x01) // max group sequence
		buf.WriteByte(0x00) // extra (fills to 4 bytes)
		src := bytes.NewReader(buf.Bytes())
		err := sum.Decode(src)
		assert.Error(t, err)
		assert.Error(t, err)
	})
}
