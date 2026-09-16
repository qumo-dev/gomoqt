package message

import (
	"errors"
	"io"
)

var ErrInvalidSubscribeDropMessageType = errors.New("invalid message type for SubscribeDropMessage")

// SubscribeDropMessage is sent by the publisher when a subscription range
// cannot be served. Group Start and Group End are plain absolute sequences,
// not the +1 form used in SUBSCRIBE.
//
// Wire format:
//
//	SUBSCRIBE_DROP Message {
//	  Type (varint) = 0x2
//	  Message Length (varint)
//	  Group Start (varint)
//	  Group End (varint)
//	  Error Code (varint)
//	}
type SubscribeDropMessage struct {
	GroupStart uint64
	GroupEnd   uint64
	ErrorCode  uint64
}

func (sdm SubscribeDropMessage) Len() int {
	var l int

	l += VarintLen(sdm.GroupStart)
	l += VarintLen(sdm.GroupEnd)
	l += VarintLen(sdm.ErrorCode)

	return l
}

func (sdm SubscribeDropMessage) Encode(w io.Writer) error {
	msgLen := sdm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, sdm.GroupStart)
	b, _ = WriteVarint(b, sdm.GroupEnd)
	b, _ = WriteVarint(b, sdm.ErrorCode)

	_, err := w.Write(b)
	return err
}

func (sdm *SubscribeDropMessage) Decode(src io.Reader) error {
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
	sdm.GroupStart = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sdm.GroupEnd = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sdm.ErrorCode = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
