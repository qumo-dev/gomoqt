package message

import (
	"errors"
	"io"
)

var ErrInvalidSubscribeOkMessageType = errors.New("invalid message type for SubscribeOkMessage")

// SubscribeOkMessage is the publisher response to SUBSCRIBE.
// The first encoded field is a type tag, fixed to 0x0 for SUBSCRIBE_OK.
type SubscribeOkMessage struct {
	PublisherPriority   uint8
	PublisherOrdered    uint8
	PublisherMaxLatency uint64
	StartGroup          uint64
	EndGroup            uint64
}

const subscribeOkMessageType uint64 = 0x0

func (som SubscribeOkMessage) Len() int {
	var l int

	l += VarintLen(subscribeOkMessageType)
	l += VarintLen(uint64(som.PublisherPriority))
	l += VarintLen(uint64(som.PublisherOrdered))
	l += VarintLen(som.PublisherMaxLatency)
	l += VarintLen(som.StartGroup)
	l += VarintLen(som.EndGroup)

	return l
}

func (som SubscribeOkMessage) Encode(w io.Writer) error {
	msgLen := som.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, subscribeOkMessageType)
	b, _ = WriteVarint(b, uint64(som.PublisherPriority))
	b, _ = WriteVarint(b, uint64(som.PublisherOrdered))
	b, _ = WriteVarint(b, som.PublisherMaxLatency)
	b, _ = WriteVarint(b, som.StartGroup)
	b, _ = WriteVarint(b, som.EndGroup)

	_, err := w.Write(b)

	return err
}

func (som *SubscribeOkMessage) Decode(src io.Reader) error {
	num, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	b := make([]byte, num)
	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	var n int
	msgType, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	b = b[n:]

	if msgType != subscribeOkMessageType {
		return ErrInvalidSubscribeOkMessageType
	}

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	som.PublisherPriority = uint8(num)
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	som.PublisherOrdered = uint8(num)
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	som.PublisherMaxLatency = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	som.StartGroup = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	som.EndGroup = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
