package message

import (
	"errors"
	"io"
)

// GroupMessage opens a Group Stream.
//
//	GROUP Message {
//	  Message Length (i)
//	  Subscribe ID (i)
//	  Group Sequence (i)
//	  Frame Start (i)
//	}
//
// Frame Start is the index within the group of the first FRAME on the stream;
// 0, the common case, means the stream carries the group from its beginning.
type GroupMessage struct {
	SubscribeID   uint64
	GroupSequence uint64
	FrameStart    uint64
}

func (g GroupMessage) Len() int {
	var l int

	l += VarintLen(uint64(g.SubscribeID))
	l += VarintLen(uint64(g.GroupSequence))
	l += VarintLen(g.FrameStart)

	return l
}

func (g GroupMessage) Encode(w io.Writer) error {
	msgLen := g.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, g.SubscribeID)
	b, _ = WriteVarint(b, g.GroupSequence)
	b, _ = WriteVarint(b, g.FrameStart)

	_, err := w.Write(b)

	return err
}

func (g *GroupMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
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

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	g.FrameStart = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}

var ErrMessageTooShort = errors.New("message too short")
