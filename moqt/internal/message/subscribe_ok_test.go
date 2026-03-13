package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
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
				PublisherPriority:   1,
				PublisherOrdered:    0,
				PublisherMaxLatency: 100,
				StartGroup:          0,
				EndGroup:            0,
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
		buf.WriteByte(0x01) // length varint = 1
		buf.WriteByte(0x00) // extra (fills to 1 byte)
		src := bytes.NewReader(buf.Bytes())
		err := som.Decode(src)
		assert.Error(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		var som message.SubscribeOkMessage
		var buf bytes.Buffer
		buf.WriteByte(0x06) // message length
		buf.WriteByte(0x01) // type=1 (invalid; must be 0)
		buf.WriteByte(0x01) // PublisherPriority
		buf.WriteByte(0x00) // PublisherOrdered
		buf.WriteByte(0x00) // PublisherMaxLatency
		buf.WriteByte(0x00) // StartGroup
		buf.WriteByte(0x00) // EndGroup
		src := bytes.NewReader(buf.Bytes())
		err := som.Decode(src)
		assert.Error(t, err)
	})
}
