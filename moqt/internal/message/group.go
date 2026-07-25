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
	b = g.AppendEncode(b)

	_, err := w.Write(b)

	return err
}

// AppendEncode appends the length-prefixed GROUP message to b and returns the
// extended slice. It encodes the byte-identical output of Encode but writes into
// a caller-provided buffer, so it allocates nothing when b has spare capacity.
// This lets callers coalesce the message with a preceding stream-type header
// into a single Write (see TrackWriter.openGroupWithSequence).
func (g GroupMessage) AppendEncode(b []byte) []byte {
	msgLen := g.Len()
	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, g.SubscribeID)
	b, _ = WriteVarint(b, g.GroupSequence)
	return b
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
