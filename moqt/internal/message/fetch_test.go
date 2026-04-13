package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.FetchMessage
	}{
		"basic_message": {
			input: message.FetchMessage{
				BroadcastPath: "/live",
				TrackName:     "video",
				Priority:      5,
				GroupSequence: 42,
			},
		},
		"empty_strings": {
			input: message.FetchMessage{
				BroadcastPath: "",
				TrackName:     "",
				Priority:      0,
				GroupSequence: 0,
			},
		},
		"large_group_sequence": {
			input: message.FetchMessage{
				BroadcastPath: "/broadcast/path",
				TrackName:     "audio",
				Priority:      255,
				GroupSequence: 1 << 48,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			err := tc.input.Encode(&buf)
			require.NoError(t, err)

			var decoded message.FetchMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			assert.Equal(t, tc.input, decoded)
		})
	}
}

func TestFetchMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty_reader": {
			data: []byte{},
		},
		"truncated_length": {
			data: []byte{0xff},
		},
		"truncated_body": {
			data: []byte{0x10, 0x01}, // length=16 but only 1 byte
		},
		"truncated_broadcast_path": {
			data: []byte{0x02, 0x0a, 0x00}, // length=2, string len=10 but 0 bytes
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			buf := bytes.NewReader(tc.data)
			var fm message.FetchMessage
			err := fm.Decode(buf)
			assert.Error(t, err)
		})
	}
}

func TestFetchMessage_Len(t *testing.T) {
	msg := message.FetchMessage{
		BroadcastPath: "/live",
		TrackName:     "video",
		Priority:      5,
		GroupSequence: 42,
	}

	var buf bytes.Buffer
	err := msg.Encode(&buf)
	require.NoError(t, err)

	// Len should match the payload size (excluding the message length prefix)
	assert.Greater(t, msg.Len(), 0)
}
