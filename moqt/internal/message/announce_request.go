package message

import (
	"io"
)

/*
 *	ANNOUNCE_REQUEST Message {
 *	  Message Length (i)
 *	  Broadcast Path Prefix (s),
 *	  Exclude Hop (i),
 *	}
 */
type AnnounceRequestMessage struct {
	BroadcastPathPrefix string
	ExcludeHop          uint64
}

func (aim AnnounceRequestMessage) Len() int {
	return StringLen(aim.BroadcastPathPrefix) + VarintLen(aim.ExcludeHop)
}

func (aim AnnounceRequestMessage) Encode(w io.Writer) error {
	msgLen := aim.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, aim.BroadcastPathPrefix)
	b, _ = WriteVarint(b, aim.ExcludeHop)

	_, err := w.Write(b)

	return err
}

func (aim *AnnounceRequestMessage) Decode(src io.Reader) error {
	num, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if num > MaxMessageSize {
		return ErrMessageTooLarge
	}

	b := make([]byte, num)

	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	aim.BroadcastPathPrefix = str
	b = b[n:]

	excludeHop, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	aim.ExcludeHop = excludeHop
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
