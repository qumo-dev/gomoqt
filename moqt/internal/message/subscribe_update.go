package message

import (
	"io"
)

// SubscribeUpdateMessage updates the subscriber-side delivery preferences and
// range of an existing SUBSCRIBE. The fields are encoded as in SUBSCRIBE.
//
//	SUBSCRIBE_UPDATE Message {
//	  Message Length (i)
//	  Subscriber Priority (8)
//	  Subscriber Max Age (i)
//	  Group Start (i)
//	  Group End (i)
//	  Frame Start (i)
//	  Frame End (i)
//	}
type SubscribeUpdateMessage struct {
	SubscriberPriority uint8
	SubscriberMaxAge   uint64
	GroupStart         uint64
	GroupEnd           uint64
	FrameStart         uint64
	FrameEnd           uint64
}

func (su SubscribeUpdateMessage) Len() int {
	var l int

	l += 1 // SubscriberPriority (uint8)
	l += VarintLen(su.SubscriberMaxAge)
	l += VarintLen(su.GroupStart)
	l += VarintLen(su.GroupEnd)
	l += VarintLen(su.FrameStart)
	l += VarintLen(su.FrameEnd)

	return l
}

func (su SubscribeUpdateMessage) Encode(w io.Writer) error {
	msgLen := su.Len()
	p := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	p, _ = WriteMessageLength(p, uint64(msgLen))
	p = append(p, su.SubscriberPriority)
	p, _ = WriteVarint(p, su.SubscriberMaxAge)
	p, _ = WriteVarint(p, su.GroupStart)
	p, _ = WriteVarint(p, su.GroupEnd)
	p, _ = WriteVarint(p, su.FrameStart)
	p, _ = WriteVarint(p, su.FrameEnd)

	_, err := w.Write(p)

	return err
}

func (sum *SubscribeUpdateMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	if len(b) < 1 {
		return ErrMessageTooShort
	}
	sum.SubscriberPriority = b[0]
	b = b[1:]

	for _, field := range [...]*uint64{&sum.SubscriberMaxAge, &sum.GroupStart, &sum.GroupEnd, &sum.FrameStart, &sum.FrameEnd} {
		num, n, err := ReadVarint(b)
		if err != nil {
			return err
		}
		*field = num
		b = b[n:]
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
