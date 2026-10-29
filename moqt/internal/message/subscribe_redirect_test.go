package message_test

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeRedirectMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input message.SubscribeRedirectMessage
	}{
		"same session": {
			input: message.SubscribeRedirectMessage{BroadcastPath: "/acme/cam", TrackName: "video"},
		},
		"another session, cached": {
			input: message.SubscribeRedirectMessage{
				ConnectURI:    "moqt://relay-b.example/",
				BroadcastPath: "/acme/cam",
				TrackName:     "catalog",
				MaxAge:        30_000,
			},
		},
		"all fields empty": {
			input: message.SubscribeRedirectMessage{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tt.input.Encode(&buf))
			assert.Equal(t, tt.input.Len()+1, buf.Len(), "a one-byte length prefix and the body")

			var decoded message.SubscribeRedirectMessage
			require.NoError(t, decoded.Decode(&buf))
			assert.Equal(t, tt.input, decoded)
		})
	}
}

func TestSubscribeRedirectMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data []byte
	}{
		"empty input":      {data: []byte{}},
		"missing max age":  {data: []byte{0x03, 0x00, 0x00, 0x00}},
		"string past body": {data: []byte{0x04, 0x00, 0x05, 0x00, 0x00}},
		"trailing bytes":   {data: []byte{0x05, 0x00, 0x00, 0x00, 0x00, 0xFF}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded message.SubscribeRedirectMessage
			assert.Error(t, decoded.Decode(bytes.NewReader(tt.data)))
		})
	}
}

func TestSetupMessage_FollowsRedirect(t *testing.T) {
	var with message.SetupMessage
	with.AddProbe(message.ProbeLevelReport)
	with.AddRedirect()

	var buf bytes.Buffer
	require.NoError(t, with.Encode(&buf))
	var decoded message.SetupMessage
	require.NoError(t, decoded.Decode(&buf))

	assert.True(t, decoded.FollowsRedirect())
	assert.False(t, message.SetupMessage{}.FollowsRedirect())
}
