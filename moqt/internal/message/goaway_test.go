package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoawayMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.GoawayMessage
		wantErr bool
	}{
		"empty uri": {
			input: message.GoawayMessage{
				NewSessionURI: "",
			},
		},
		"with redirect uri": {
			input: message.GoawayMessage{
				NewSessionURI: "https://new.example.com/moq",
			},
		},
		"long uri": {
			input: message.GoawayMessage{
				NewSessionURI: "https://cdn.example.com/very/long/path/to/new/session",
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

			var decoded message.GoawayMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestGoawayMessage_DecodeErrors(t *testing.T) {
	t.Run("read message length error", func(t *testing.T) {
		var gm message.GoawayMessage
		src := bytes.NewReader([]byte{})
		err := gm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read full error", func(t *testing.T) {
		var gm message.GoawayMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 10)
		buf.WriteByte(0x00)
		src := bytes.NewReader(buf.Bytes()[:2])
		err := gm.Decode(src)
		assert.Error(t, err)
	})

	t.Run("read string error", func(t *testing.T) {
		var gm message.GoawayMessage
		var buf bytes.Buffer
		buf.WriteByte(0x80 | 1)
		buf.WriteByte(0x00)
		buf.WriteByte(0x80) // invalid string
		src := bytes.NewReader(buf.Bytes())
		err := gm.Decode(src)
		assert.Error(t, err)
	})
}
