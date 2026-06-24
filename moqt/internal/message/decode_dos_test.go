package message_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
)

// TestDecode_RejectsOversizedLength ensures every length-prefixed message rejects
// an attacker-controlled length prefix that exceeds MaxMessageSize, returning
// ErrMessageTooLarge before allocating the payload buffer. Without this guard a peer
// can advertise a maxUint62 length and force make([]byte, ~4.6e18) -> OOM crash.
func TestDecode_RejectsOversizedLength(t *testing.T) {
	// Largest value expressible in a QUIC varint (uint62 max), encoded as the
	// message length prefix. No payload bytes follow.
	lengthPrefix, _ := message.WriteMessageLength(nil, 1<<62-1)

	decoders := map[string]interface{ Decode(io.Reader) error }{
		"AnnounceMessage":         &message.AnnounceMessage{},
		"AnnounceInterestMessage": &message.AnnounceInterestMessage{},
		"FetchMessage":            &message.FetchMessage{},
		"GoawayMessage":           &message.GoawayMessage{},
		"GroupMessage":            &message.GroupMessage{},
		"ProbeMessage":            &message.ProbeMessage{},
		"SubscribeMessage":        &message.SubscribeMessage{},
		"SubscribeDropMessage":    &message.SubscribeDropMessage{},
		"SubscribeOkMessage":      &message.SubscribeOkMessage{},
		"SubscribeUpdateMessage":  &message.SubscribeUpdateMessage{},
	}

	for name, dec := range decoders {
		t.Run(name, func(t *testing.T) {
			err := dec.Decode(bytes.NewReader(lengthPrefix))
			assert.ErrorIs(t, err, message.ErrMessageTooLarge)
		})
	}
}
