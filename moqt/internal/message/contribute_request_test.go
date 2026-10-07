package message

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContributeRequestMessage_EncodeDecode(t *testing.T) {
	tests := map[string]ContributeRequestMessage{
		"path and track": {BroadcastPath: "/room/1/chat", TrackName: "alice"},
		"empty track":    {BroadcastPath: "/room/1/chat"},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, want.Encode(&buf))
			assert.Equal(t, want.Len()+VarintLen(uint64(want.Len())), buf.Len())

			var got ContributeRequestMessage
			require.NoError(t, got.Decode(&buf))

			assert.Equal(t, want, got)
		})
	}
}

// TestContributeRequestMessage_SharesTrackLayout pins the wire layout to
// TRACK's, so a change to one is noticed in the other.
func TestContributeRequestMessage_SharesTrackLayout(t *testing.T) {
	var request, track bytes.Buffer
	require.NoError(t, ContributeRequestMessage{BroadcastPath: "/room/1/chat", TrackName: "alice"}.Encode(&request))
	require.NoError(t, TrackMessage{BroadcastPath: "/room/1/chat", TrackName: "alice"}.Encode(&track))

	assert.Equal(t, track.Bytes(), request.Bytes())
}
