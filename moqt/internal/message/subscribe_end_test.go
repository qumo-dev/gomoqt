package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeEndMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.SubscribeEndMessage
	}{
		"zero group (track ended with no groups)": {
			input: message.SubscribeEndMessage{Group: 0},
		},
		"last group": {
			input: message.SubscribeEndMessage{Group: 42},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			require.NoError(t, tt.input.Encode(&buf))

			var decoded message.SubscribeEndMessage
			require.NoError(t, decoded.Decode(&buf))

			assert.Equal(t, tt.input, decoded)
		})
	}
}

func TestSubscribeEndMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty input":    {data: []byte{}},
		"trailing bytes": {data: []byte{0x02, 0x01, 0xFF}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded message.SubscribeEndMessage
			err := decoded.Decode(bytes.NewReader(tt.data))
			assert.Error(t, err)
		})
	}
}
