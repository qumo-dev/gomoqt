package message

import (
	"bytes"
	"testing"
)

func TestDoS(t *testing.T) {
	varintBytes, _ := WriteMessageLength(nil, 1<<62-1)
	r := bytes.NewReader(varintBytes)

	msg := SubscribeMessage{}
	err := msg.Decode(r)
	if err != ErrMessageTooLarge {
		t.Fatalf("expected ErrMessageTooLarge, got: %v", err)
	}
}
