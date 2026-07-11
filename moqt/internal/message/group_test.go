package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.GroupMessage
		wantErr bool
	}{
		"valid message": {
			input: message.GroupMessage{
				SubscribeID:   1,
				GroupSequence: 1,
			},
		},
		"max values": {
			input: message.GroupMessage{
				SubscribeID:   1<<(64-2) - 1, // maxVarInt8 (uint62 max)
				GroupSequence: 1<<(64-2) - 1, // maxVarInt8 (uint62 max)
			},
		},
		"zero values": {
			input: message.GroupMessage{
				SubscribeID:   0,
				GroupSequence: 0,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			// Encode
			func() {
				defer func() {
					if r := recover(); r != nil {
						if tc.wantErr {
							// Expected panic, treat as error
							return
						}
						panic(r) // Re-panic if not expected
					}
				}()
				err := tc.input.Encode(&buf)
				if tc.wantErr {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
			}()

			if tc.wantErr {
				return // Skip decode for error cases
			}

			// Decode
			var decoded message.GroupMessage
			err := decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestGroupMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var g message.GroupMessage
		src := bytes.NewReader([]byte{})
		err := g.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var g message.GroupMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := g.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for subscribe id", func(t *testing.T) {
		var g message.GroupMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1)
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid varint
		src := bytes.NewReader(buf.Bytes())
		err := g.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read varint error for group sequence", func(t *testing.T) {
		var g message.GroupMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 3)
		buf.WriteByte(0x00)
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x80) // invalid varint for group sequence
		src := bytes.NewReader(buf.Bytes())
		err := g.Decode(src)
		assert.Error(t, err)
	})

	t.Run("extra data", func(t *testing.T) {
		var g message.GroupMessage
		var buf bytes.Buffer
		buf.WriteByte(0x03) // length varint = 3
		buf.WriteByte(0x01) // subscribe id
		buf.WriteByte(0x01) // group sequence
		buf.WriteByte(0x00) // extra (fills to 3 bytes)
		src := bytes.NewReader(buf.Bytes())
		err := g.Decode(src)
		assert.Error(t, err)
		assert.Equal(t, message.ErrMessageTooShort, err)
	})
}
