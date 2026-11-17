package message

import (
	"io"
)

/*
 *	ANNOUNCE_REQUEST Message {
 *	  Message Length (i)
 *	  Broadcast Path Prefix (s),
 *	}
 *
 * moq-lite-06 removed Exclude Hop: the receiver's hop-based loop check and
 * the SETUP Hop parameter cover it.
 */
type AnnounceRequestMessage struct {
	BroadcastPathPrefix string
}

func (aim AnnounceRequestMessage) Len() int {
	return StringLen(aim.BroadcastPathPrefix)
}

func (aim AnnounceRequestMessage) Encode(w io.Writer) error {
	msgLen := aim.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, aim.BroadcastPathPrefix)

	_, err := w.Write(b)

	return err
}

func (aim *AnnounceRequestMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	aim.BroadcastPathPrefix = str
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
