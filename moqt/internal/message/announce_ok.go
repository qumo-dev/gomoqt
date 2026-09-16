package message

import (
	"io"
)

/*
 *	ANNOUNCE_OK Message {
 *	  Message Length (i)
 *	  Hop ID (i)
 *	  Active Count (i)
 *	}
 */
type AnnounceOkMessage struct {
	// HopID is the publisher's own Hop ID. It is the implicit trailing
	// entry of every ANNOUNCE_BROADCAST's Hop ID list on this stream.
	// 0 means unknown (non-relay endpoints send 0).
	HopID uint64
	// ActiveCount is the number of active ANNOUNCE_BROADCAST messages the
	// publisher will send immediately as the initial set.
	ActiveCount uint64
}

func (aom AnnounceOkMessage) Len() int {
	return VarintLen(aom.HopID) + VarintLen(aom.ActiveCount)
}

func (aom AnnounceOkMessage) Encode(w io.Writer) error {
	msgLen := aom.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, aom.HopID)
	b, _ = WriteVarint(b, aom.ActiveCount)

	_, err := w.Write(b)

	return err
}

func (aom *AnnounceOkMessage) Decode(src io.Reader) error {
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
	aom.HopID = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	aom.ActiveCount = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
