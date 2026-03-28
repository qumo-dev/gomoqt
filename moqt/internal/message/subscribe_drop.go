package message

import (
	"errors"
	"io"
)

var ErrInvalidSubscribeDropMessageType = errors.New("invalid message type for SubscribeDropMessage")

// SubscribeDropMessage is sent by the publisher when a subscription range
// cannot be served.
//
// Wire format:
//
//	SUBSCRIBE_DROP Message {
//	  Message Length (varint)
//	  Type (varint) = 0x1
//	  Start Group (varint)
//	  End Group (varint)
//	  Error Code (varint)
//	}
type SubscribeDropMessage struct {
	StartGroup uint64
	EndGroup   uint64
	ErrorCode  uint64
}

func (sdm SubscribeDropMessage) Len() int {
	var l int

	l += VarintLen(0x1)
	l += VarintLen(sdm.StartGroup)
	l += VarintLen(sdm.EndGroup)
	l += VarintLen(sdm.ErrorCode)

	return l
}

func (sdm SubscribeDropMessage) Encode(w io.Writer) error {
	msgLen := sdm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, 0x1)
	b, _ = WriteVarint(b, sdm.StartGroup)
	b, _ = WriteVarint(b, sdm.EndGroup)
	b, _ = WriteVarint(b, sdm.ErrorCode)

	_, err := w.Write(b)
	return err
}

func (sdm *SubscribeDropMessage) Decode(src io.Reader) error {
	size, err := ReadMessageLength(src)
	if err != nil {
		return err
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
	if num != 0x1 {
		return ErrInvalidSubscribeDropMessageType
	}
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sdm.StartGroup = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	sdm.EndGroup = num
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
