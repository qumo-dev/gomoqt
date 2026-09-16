package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackInfoMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.TrackInfoMessage
	}{
		"millisecond timescale": {
			input: message.TrackInfoMessage{
				PublisherPriority:   5,
				PublisherOrdered:    1,
				PublisherMaxLatency: 2000,
				Timescale:           1000,
			},
		},
		"rtp video clock": {
			input: message.TrackInfoMessage{
				PublisherPriority: 128,
				Timescale:         90000,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			require.NoError(t, tt.input.Encode(&buf))

			var decoded message.TrackInfoMessage
			require.NoError(t, decoded.Decode(&buf))

			assert.Equal(t, tt.input, decoded)
		})
	}
}

func TestTrackInfoMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty input":     {data: []byte{}},
		"too short":       {data: []byte{0x01, 0x05}},
		"trailing bytes": {data: []byte{0x05, 0x01, 0x00, 0x00, 0x00, 0xFF}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded message.TrackInfoMessage
			err := decoded.Decode(bytes.NewReader(tt.data))
			assert.Error(t, err)
		})
	}
}
