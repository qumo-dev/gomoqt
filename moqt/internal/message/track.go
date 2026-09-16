package message

import (
	"io"
)

/*
 *	TRACK Message {
 *	  Message Length (i)
 *	  Broadcast Path (s)
 *	  Track Name (s)
 *	}
 */
type TrackMessage struct {
	BroadcastPath string
	TrackName     string
}

func (tm TrackMessage) Len() int {
	return StringLen(tm.BroadcastPath) + StringLen(tm.TrackName)
}

func (tm TrackMessage) Encode(w io.Writer) error {
	msgLen := tm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, tm.BroadcastPath)
	b, _ = WriteString(b, tm.TrackName)

	_, err := w.Write(b)

	return err
}

func (tm *TrackMessage) Decode(src io.Reader) error {
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

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	tm.BroadcastPath = str
	b = b[n:]

	str, n, err = ReadString(b)
	if err != nil {
		return err
	}
	tm.TrackName = str
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
