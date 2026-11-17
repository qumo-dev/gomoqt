package message

import (
	"io"
)

// SubscribeEndMessage is sent by the publisher to signal that no group at or
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
	// Group is the exclusive end of the range: the absolute sequence of the
	// first group that will never be delivered. 0 means the track ended
	// before producing any group.
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
	b, err := readBody(src)
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
