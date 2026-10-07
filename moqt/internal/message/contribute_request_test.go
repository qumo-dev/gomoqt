package message

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContributeRequestMessage_EncodeDecode(t *testing.T) {
	tests := map[string]ContributeRequestMessage{
		"path and track": {BroadcastPath: "/room/1/chat", TrackName: "alice"},
		"empty track":    {BroadcastPath: "/room/1/chat"},
		"empty message":  {},
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

func TestContributeRequestMessage_Decode_Malformed(t *testing.T) {
	var valid bytes.Buffer
	require.NoError(t, ContributeRequestMessage{BroadcastPath: "/room/1/chat", TrackName: "alice"}.Encode(&valid))

	// A payload with a byte left over after both strings: the declared length
	// is one more than the fields account for.
	body := valid.Bytes()[1:]
	trailing, _ := WriteMessageLength(nil, uint64(len(body)+1))
	trailing = append(append(trailing, body...), 0)

	tests := map[string]struct {
		input []byte
		want  error
	}{
		"no bytes":             {input: nil, want: io.EOF},
		"truncated payload":    {input: valid.Bytes()[:valid.Len()-1], want: io.ErrUnexpectedEOF},
		"trailing byte":        {input: trailing, want: ErrMessageTooShort},
		"missing track string": {input: append([]byte{1}, 0), want: io.EOF},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got ContributeRequestMessage

			err := got.Decode(bytes.NewReader(tt.input))

			assert.ErrorIs(t, err, tt.want)
		})
	}
}
