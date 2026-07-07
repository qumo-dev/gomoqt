package message

import (
	"io"
)

// SubscribeUpdateMessage updates the subscriber-side delivery preferences and
// group range for an existing SUBSCRIBE.
type SubscribeUpdateMessage struct {
	SubscriberPriority   uint8
	SubscriberOrdered    uint8
	SubscriberMaxLatency uint64
	StartGroup           uint64
	EndGroup             uint64
}

func (su SubscribeUpdateMessage) Len() int {
	var l int

	l += 1 // SubscriberPriority (uint8)
	l += 1 // SubscriberOrdered (uint8)
	l += VarintLen(su.SubscriberMaxLatency)
	l += VarintLen(su.StartGroup)
	l += VarintLen(su.EndGroup)

	return l
}

func (su SubscribeUpdateMessage) Encode(w io.Writer) error {
	var err error
	msgLen := su.Len()
	p := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	p, _, err = WriteMessageLength(p, uint64(msgLen))
	if err != nil {
		return err
	}
	p = append(p, su.SubscriberPriority)
	p = append(p, su.SubscriberOrdered)
	p, _, err = WriteVarint(p, su.SubscriberMaxLatency)
	if err != nil {
		return err
	}
	p, _, err = WriteVarint(p, su.StartGroup)
	if err != nil {
		return err
	}
	p, _, err = WriteVarint(p, su.EndGroup)
	if err != nil {
		return err
	}

	_, err = w.Write(p)

	return err
}

func (sum *SubscribeUpdateMessage) Decode(src io.Reader) error {
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

	if len(b) < 2 {
		return ErrMessageTooShort
	}
	sum.SubscriberPriority = b[0]
	sum.SubscriberOrdered = b[1]
	b = b[2:]

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	sum.SubscriberMaxLatency = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sum.StartGroup = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sum.EndGroup = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
