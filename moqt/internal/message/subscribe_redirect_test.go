package message_test

import (
	"bytes"
	"strings"
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
		"another session": {
			input: message.SubscribeRedirectMessage{
				ConnectURI:    "moqt://relay-b.example/",
				BroadcastPath: "/acme/cam",
				TrackName:     "catalog",
			},
		},
		"all fields empty": {
			input: message.SubscribeRedirectMessage{},
		},
		"long path and non-ASCII name": {
			input: message.SubscribeRedirectMessage{
				BroadcastPath: "/" + strings.Repeat("segment/", 200) + "cam",
				TrackName:     "映像",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tt.input.Encode(&buf))
			prefix, _ := message.WriteMessageLength(nil, uint64(tt.input.Len()))
			assert.Equal(t, len(prefix)+tt.input.Len(), buf.Len(), "the length prefix and the body")

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
		"empty input":        {data: []byte{}},
		"missing track name": {data: []byte{0x02, 0x00, 0x00}},
		"string past body":   {data: []byte{0x03, 0x00, 0x05, 0x00}},
		"trailing bytes":     {data: []byte{0x04, 0x00, 0x00, 0x00, 0xFF}},
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

func TestSetupMessage_FollowsRedirect_Version(t *testing.T) {
	tests := map[string]struct {
		value []byte
		want  bool
	}{
		"empty is version 0": {value: nil, want: true},
		"version 0":          {value: []byte{0x00}, want: true},
		"a later version":    {value: []byte{0x02}, want: true},
		"truncated varint":   {value: []byte{0x40}, want: false},
		"trailing bytes":     {value: []byte{0x01, 0x01}, want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sm := message.SetupMessage{Parameters: []message.SetupParameter{{ID: message.SetupParamRedirect, Value: tt.value}}}
			assert.Equal(t, tt.want, sm.FollowsRedirect())
		})
	}
}

// FuzzSubscribeRedirectMessage_Decode checks that no input panics the decoder,
// and that whatever it accepts encodes back to the same message.
func FuzzSubscribeRedirectMessage_Decode(f *testing.F) {
	for _, seed := range []message.SubscribeRedirectMessage{
		{},
		{BroadcastPath: "/real", TrackName: "video"},
		{ConnectURI: "moqt://relay-b.example/", BroadcastPath: "/acme/cam", TrackName: "catalog"},
	} {
		var buf bytes.Buffer
		require.NoError(f, seed.Encode(&buf))
		f.Add(buf.Bytes())
	}
	f.Add([]byte{0x03, 0x05, 0x00, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		var decoded message.SubscribeRedirectMessage
		if err := decoded.Decode(bytes.NewReader(data)); err != nil {
			return
		}
		var buf bytes.Buffer
		require.NoError(t, decoded.Encode(&buf))
		var again message.SubscribeRedirectMessage
		require.NoError(t, again.Decode(&buf))
		assert.Equal(t, decoded, again)
	})
}
