package message

import (
	"bytes"
	"io"
	"testing"
)

func TestDoS(t *testing.T) {
	varintBytes, _ := WriteMessageLength(nil, 1<<62-1)

	messages := []interface{ Decode(io.Reader) error }{
		&AnnounceMessage{},
		&AnnounceInterestMessage{},
		&FetchMessage{},
		&GoawayMessage{},
		&GroupMessage{},
		&ProbeMessage{},
		&SubscribeMessage{},
		&SubscribeDropMessage{},
		&SubscribeOkMessage{},
		&SubscribeUpdateMessage{},
	}

	for _, msg := range messages {
		r := bytes.NewReader(varintBytes)
		err := msg.Decode(r)
		if err != ErrMessageTooLarge {
			t.Errorf("expected ErrMessageTooLarge for %T, got: %v", msg, err)
		}
	}
}
