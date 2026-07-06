package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeOkMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.SubscribeOkMessage
		wantErr bool
	}{
		"valid message": {
			input: message.SubscribeOkMessage{
				Group: 42,
			},
		},
		"zero group": {
			input: message.SubscribeOkMessage{
				Group: 0,
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
			var decoded message.SubscribeOkMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare all fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestSubscribeOkMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var som message.SubscribeOkMessage
		src := bytes.NewReader([]byte{})
		err := som.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var som message.SubscribeOkMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := som.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra data", func(t *testing.T) {
		var som message.SubscribeOkMessage
		var buf bytes.Buffer
		// A message longer than its single Group field must be rejected;
		// the version and extensions carry new fields, not the length.
		buf.WriteByte(0x02) // length varint = 2
		buf.WriteByte(0x0a) // Group = 10
		buf.WriteByte(0x00) // unexpected extra byte inside the message
		src := bytes.NewReader(buf.Bytes())
		err := som.Decode(src)
		assert.ErrorIs(t, err, message.ErrMessageTooShort)
	})
}
