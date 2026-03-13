package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.SubscribeMessage
		wantErr bool
	}{
		"valid message": {
			input: message.SubscribeMessage{
				SubscribeID:        1,
				BroadcastPath:      "path/to/track",
				SubscriberPriority: 5,
			},
		},
		"empty track path": {
			input: message.SubscribeMessage{
				SubscribeID:        1,
				BroadcastPath:      "",
				SubscriberPriority: 5,
			},
		},
		"nil parameters": {
			input: message.SubscribeMessage{
				SubscribeID:        1,
				BroadcastPath:      "path",
				SubscriberPriority: 1,
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
			var decoded message.SubscribeMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare all fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestSubscribeMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var s message.SubscribeMessage
		src := bytes.NewReader([]byte{})
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for subscribe id", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1)
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read string error for broadcast path", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 3)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x80) // invalid string
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read string error for track name", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 5)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // broadcast path length 1
		buf.WriteByte('a')
		buf.WriteByte(0x80) // invalid string for track name
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for track priority", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 7)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // broadcast path length 1
		buf.WriteByte('a')
		buf.WriteByte(0x01) // track name length 1
		buf.WriteByte('b')
		buf.WriteByte(0x80) // invalid varint for track priority
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for min group sequence", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 8)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // broadcast path length 1
		buf.WriteByte('a')
		buf.WriteByte(0x01) // track name length 1
		buf.WriteByte('b')
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x80) // invalid varint for min group sequence
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for max group sequence", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 9)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // broadcast path length 1
		buf.WriteByte('a')
		buf.WriteByte(0x01) // track name length 1
		buf.WriteByte('b')
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x01) // min group sequence
		buf.WriteByte(0x80) // invalid varint for max group sequence
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra data", func(t *testing.T) {
		var s message.SubscribeMessage
		var buf bytes.Buffer
		buf.WriteByte(0x0A) // length varint = 10
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // broadcast path length 1
		buf.WriteByte('a')
		buf.WriteByte(0x01) // track name length 1
		buf.WriteByte('b')
		buf.WriteByte(0x01) // track priority
		buf.WriteByte(0x01) // min group sequence
		buf.WriteByte(0x01) // max group sequence
		buf.WriteByte(0xFF) // extra byte 1 (total message 9 + 1 extra = 10)
		buf.WriteByte(0xFF) // extra byte 2 (need 10 bytes in the buffer for ReadFull)
		src := bytes.NewReader(buf.Bytes())
		err := s.Decode(src)
		assert.Error(t, err)
		assert.Error(t, err)
	})
}
