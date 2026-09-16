package message

import (
	"errors"
	"io"
)

var ErrInvalidSubscribeOkMessageType = errors.New("invalid message type for SubscribeOkMessage")

// Type tags for messages sent by the publisher on the Subscribe Stream.
// The type varint is written by the caller before Encode / consumed before Decode.
const (
	MessageTypeSubscribeOk   uint64 = 0x0
	MessageTypeSubscribeEnd  uint64 = 0x1
	MessageTypeSubscribeDrop uint64 = 0x2
)

// SubscribeOkMessage confirms a subscription and resolves its absolute
// start group. It is the first message the publisher sends on the
// Subscribe Stream, once the start group is known.
//
// Wire format:
//
//	SUBSCRIBE_OK Message {
//	  Type (i) = 0x0
//	  Message Length (i)
//	  Group (i)
//	}
type SubscribeOkMessage struct {
	// Group is the absolute sequence number of the first group that will
	// be delivered (plain absolute sequence, not the +1 form of SUBSCRIBE).
	Group uint64
}

func (som SubscribeOkMessage) Len() int {
	return VarintLen(som.Group)
}

func (som SubscribeOkMessage) Encode(w io.Writer) error {
	msgLen := som.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, som.Group)

	_, err := w.Write(b)

	return err
}

func (som *SubscribeOkMessage) Decode(src io.Reader) error {
	num, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if num > MaxMessageSize {
		return ErrMessageTooLarge
	}

	b := make([]byte, num)
	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	som.Group = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
