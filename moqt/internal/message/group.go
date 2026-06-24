package message

import (
	"errors"
	"io"
)

type GroupMessage struct {
	SubscribeID   uint64
	GroupSequence uint64
}

func (g GroupMessage) Len() int {
	var l int

	l += VarintLen(uint64(g.SubscribeID))
	l += VarintLen(uint64(g.GroupSequence))

	return l
}

func (g GroupMessage) Encode(w io.Writer) error {
	msgLen := g.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, g.SubscribeID)
	b, _ = WriteVarint(b, g.GroupSequence)

	_, err := w.Write(b)

	return err
}

func (g *GroupMessage) Decode(src io.Reader) error {
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
	g.SubscribeID = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	g.GroupSequence = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}

var ErrMessageTooShort = errors.New("message too short")
