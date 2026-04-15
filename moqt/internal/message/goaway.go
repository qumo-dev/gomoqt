package message

import (
	"io"
)

/*
 *	GOAWAY Message {
 *	  Message Length (i)
 *	  New Session URI (s)
 *	}
 */
type GoawayMessage struct {
	NewSessionURI string
}

func (gm GoawayMessage) Len() int {
	return StringLen(gm.NewSessionURI)
}

func (gm GoawayMessage) Encode(w io.Writer) error {
	msgLen := gm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, gm.NewSessionURI)

	_, err := w.Write(b)

	return err
}

func (gm *GoawayMessage) Decode(src io.Reader) error {
	num, err := ReadMessageLength(src)
	if err != nil {
		return err
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
	gm.NewSessionURI = str
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
