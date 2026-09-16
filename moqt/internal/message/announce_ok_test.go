package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnounceOkMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.AnnounceOkMessage
	}{
		"zero values": {
			input: message.AnnounceOkMessage{},
		},
		"non-relay endpoint": {
			input: message.AnnounceOkMessage{HopID: 0, ActiveCount: 3},
		},
		"relay with hop id": {
			input: message.AnnounceOkMessage{HopID: 0x3FFFFFFF, ActiveCount: 128},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			require.NoError(t, tt.input.Encode(&buf))

			var decoded message.AnnounceOkMessage
			require.NoError(t, decoded.Decode(&buf))

			assert.Equal(t, tt.input, decoded)
		})
	}
}

func TestAnnounceOkMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty input":      {data: []byte{}},
		"missing fields":   {data: []byte{0x01, 0x00}}, // length 1, only HopID
		"trailing garbage": {data: []byte{0x03, 0x00, 0x00, 0xFF}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded message.AnnounceOkMessage
			err := decoded.Decode(bytes.NewReader(tt.data))
			assert.Error(t, err)
		})
	}
}
