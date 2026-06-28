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

func TestDecode_RejectsOversizedArrayCount(t *testing.T) {
	// 1. AnnounceMessage HopIDs
	// Create a payload with a large hop count but no actual hop ID bytes
	b := make([]byte, 0, 8)
	b, _ = message.WriteVarint(b, 1000000) // HopCount = 1,000,000

	// Create an AnnounceMessage with proper length prefix and structure
	payload := make([]byte, 0, 64)
	payload, _ = message.WriteVarint(payload, uint64(message.ACTIVE)) // AnnounceStatus
	payload, _ = message.WriteString(payload, "test")                 // BroadcastPathSuffix
	payload = append(payload, b...)                                   // Add the giant HopCount

	msgLen := uint64(len(payload))
	full := make([]byte, 0, 128)
	full, _ = message.WriteMessageLength(full, msgLen)
	full = append(full, payload...)

	am := &message.AnnounceMessage{}
	err := am.Decode(bytes.NewReader(full))
	assert.ErrorIs(t, err, io.EOF) // Should fail with EOF reading first missing HopID, NOT OOM crash

	// 2. ReadStringArray count
	b2 := make([]byte, 0, 8)
	b2, _ = message.WriteVarint(b2, 1000000) // Count = 1,000,000

	_, _, err2 := message.ReadStringArray(b2)
	assert.ErrorIs(t, err2, io.EOF) // Should fail with EOF reading first missing string, NOT OOM crash
}
