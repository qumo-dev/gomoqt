package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.TrackMessage
	}{
		"valid message": {
			input: message.TrackMessage{
				BroadcastPath: "/live/alice",
				TrackName:     "video",
			},
		},
		"empty fields": {
			input: message.TrackMessage{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			require.NoError(t, tt.input.Encode(&buf))

			var decoded message.TrackMessage
			require.NoError(t, decoded.Decode(&buf))

			assert.Equal(t, tt.input, decoded)
		})
	}
}

func TestTrackMessage_DecodeErrors(t *testing.T) {
	var decoded message.TrackMessage
	err := decoded.Decode(bytes.NewReader([]byte{}))
	assert.Error(t, err)
}
