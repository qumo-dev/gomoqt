package message

import (
	"io"
)

// SubscribeEndMessage is sent by the publisher to signal that no group
// after the given sequence will be produced.
//
// Wire format:
//
//	SUBSCRIBE_END Message {
//	  Type (i) = 0x1
//	  Message Length (i)
//	  Group (i)
//	}
type SubscribeEndMessage struct {
	// Group is the absolute sequence number of the last group that may be
	// delivered, inclusive (plain absolute sequence, not the +1 form).
	Group uint64
}

func (sem SubscribeEndMessage) Len() int {
	return VarintLen(sem.Group)
}

func (sem SubscribeEndMessage) Encode(w io.Writer) error {
	msgLen := sem.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, sem.Group)

	_, err := w.Write(b)

	return err
}

func (sem *SubscribeEndMessage) Decode(src io.Reader) error {
	size, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if size > MaxMessageSize {
		return ErrMessageTooLarge
	}

	b := make([]byte, size)
	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	sem.Group = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
