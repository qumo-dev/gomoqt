package message_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
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

func TestGoawayMessage_URILengthLimit(t *testing.T) {
	t.Run("encode refuses an over-long uri", func(t *testing.T) {
		gm := message.GoawayMessage{NewSessionURI: strings.Repeat("a", message.MaxNewSessionURILen+1)}
		assert.ErrorIs(t, gm.Encode(io.Discard), message.ErrNewSessionURITooLong)
	})

	t.Run("a uri at the limit round-trips", func(t *testing.T) {
		gm := message.GoawayMessage{NewSessionURI: strings.Repeat("a", message.MaxNewSessionURILen)}
		var buf bytes.Buffer
		require.NoError(t, gm.Encode(&buf))
		var decoded message.GoawayMessage
		require.NoError(t, decoded.Decode(&buf))
		assert.Equal(t, gm, decoded)
	})

	t.Run("decode rejects an over-long uri", func(t *testing.T) {
		uri := strings.Repeat("a", message.MaxNewSessionURILen+1)
		body, _ := message.WriteString(nil, uri)
		full, _ := message.WriteMessageLength(nil, uint64(len(body)))
		full = append(full, body...)

		var decoded message.GoawayMessage
		assert.ErrorIs(t, decoded.Decode(bytes.NewReader(full)), message.ErrNewSessionURITooLong)
	})

	t.Run("decode accepts a uri whose length uses a longer varint", func(t *testing.T) {
		uri := strings.Repeat("a", message.MaxNewSessionURILen-1)
		// 8191 in the 4-byte varint form, then the URI.
		body := append([]byte{0x80, 0x00, 0x1f, 0xff}, uri...)
		full, _ := message.WriteMessageLength(nil, uint64(len(body)))
		full = append(full, body...)
		var decoded message.GoawayMessage

		err := decoded.Decode(bytes.NewReader(full))

		require.NoError(t, err)
		assert.Equal(t, uri, decoded.NewSessionURI)
	})
}
