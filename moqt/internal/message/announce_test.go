package message_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typedMessage is an announcement as the Announce Stream carries it: Encode
// writes the Type tag, Decode reads what follows it.
type typedMessage interface {
	Encode(io.Writer) error
	Decode(io.Reader) error
}

func TestAnnounceMessages_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input    typedMessage
		decoded  typedMessage
		wantType uint64
	}{
		"start with hops and costs": {
			input: &message.AnnounceStartMessage{
				RoutePrefixSuffix: "alice",
				HopIDs:            []uint64{7, 0, 0, 1 << 40},
				WarmRouteCost:     3,
				ColdRouteCost:     1<<62 - 1,
			},
			decoded:  &message.AnnounceStartMessage{},
			wantType: message.MessageTypeAnnounceStart,
		},
		"start with empty suffix": {
			input:    &message.AnnounceStartMessage{HopIDs: []uint64{}},
			decoded:  &message.AnnounceStartMessage{},
			wantType: message.MessageTypeAnnounceStart,
		},
		"end": {
			input:    &message.AnnounceEndMessage{AnnounceID: 300},
			decoded:  &message.AnnounceEndMessage{},
			wantType: message.MessageTypeAnnounceEnd,
		},
		"update": {
			input: &message.AnnounceUpdateMessage{
				AnnounceID:    2,
				HopIDs:        []uint64{9},
				WarmRouteCost: 1,
				ColdRouteCost: 2,
			},
			decoded:  &message.AnnounceUpdateMessage{},
			wantType: message.MessageTypeAnnounceUpdate,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tc.input.Encode(&buf))

			typ, err := message.ReadMessageType(&buf)
			require.NoError(t, err)
			assert.Equal(t, tc.wantType, typ)

			require.NoError(t, tc.decoded.Decode(&buf))
			assert.Equal(t, tc.input, tc.decoded)
			assert.Zero(t, buf.Len(), "decode should consume the whole message")
		})
	}
}

func TestAnnounceStartMessage_Decode_HopCountMismatch(t *testing.T) {
	tests := map[string]struct {
		body []byte
	}{
		"fewer hop ids than the count": {
			// suffix "", Hop Count 2, one Hop ID, both costs
			body: []byte{0x00, 0x02, 0x05, 0x00, 0x00},
		},
		"more hop ids than the count": {
			// suffix "", Hop Count 1, two Hop IDs, both costs
			body: []byte{0x00, 0x01, 0x05, 0x06, 0x00, 0x00},
		},
		"missing cold cost": {
			body: []byte{0x00, 0x00, 0x00},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			full, _ := message.WriteMessageLength(nil, uint64(len(tc.body)))
			full = append(full, tc.body...)

			var am message.AnnounceStartMessage
			assert.Error(t, am.Decode(bytes.NewReader(full)))
		})
	}
}

func TestCheckHopIDs(t *testing.T) {
	tests := map[string]struct {
		ids     []uint64
		wantErr error
	}{
		"empty":                  {ids: nil},
		"distinct":               {ids: []uint64{1, 2, 3}},
		"repeated zeros":         {ids: []uint64{0, 4, 0, 0}},
		"repeated non-zero":      {ids: []uint64{5, 6, 5}, wantErr: message.ErrDuplicateHopID},
		"adjacent repeated hops": {ids: []uint64{0, 8, 8}, wantErr: message.ErrDuplicateHopID},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := message.CheckHopIDs(tc.ids)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
