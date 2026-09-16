package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnounceRequestMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.AnnounceRequestMessage
		wantErr bool
	}{
		"valid message": {
			input: message.AnnounceRequestMessage{
				BroadcastPathPrefix: "part1/part2",
				ExcludeHop:          0,
			},
		},
		"empty prefix": {
			input: message.AnnounceRequestMessage{
				BroadcastPathPrefix: "",
				ExcludeHop:          0,
			},
		},
		"with exclude hop": {
			input: message.AnnounceRequestMessage{
				BroadcastPathPrefix: "path",
				ExcludeHop:          42,
			},
		},
		"long path": {
			input: message.AnnounceRequestMessage{
				BroadcastPathPrefix: "very/long/path/with/many/segments",
				ExcludeHop:          12345,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			err := tc.input.Encode(&buf)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded message.AnnounceRequestMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestAnnounceRequestMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var aim message.AnnounceRequestMessage
		src := bytes.NewReader([]byte{})
		err := aim.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var aim message.AnnounceRequestMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := aim.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read string error", func(t *testing.T) {
		var aim message.AnnounceRequestMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1)
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid string
		src := bytes.NewReader(buf.Bytes())
		err := aim.Decode(src)
		assert.Error(t, err)
	})
}
