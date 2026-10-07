package message

import (
	"io"
)

/*
 *	CONTRIBUTE_REQUEST Message {
 *	  Message Length (i)
 *	  Broadcast Path (s)
 *	  Track Name (s)
 *	}
 */

// ContributeRequestMessage is the first message on a Contribute Stream: a
// publisher asks the receiver to subscribe to one track of a broadcast the
// publisher does not announce.
type ContributeRequestMessage struct {
	BroadcastPath string
	TrackName     string
}

func (crm ContributeRequestMessage) Len() int {
	return StringLen(crm.BroadcastPath) + StringLen(crm.TrackName)
}

func (crm ContributeRequestMessage) Encode(w io.Writer) error {
	msgLen := crm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, crm.BroadcastPath)
	b, _ = WriteString(b, crm.TrackName)

	_, err := w.Write(b)
	return err
}

func (crm *ContributeRequestMessage) Decode(src io.Reader) error {
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
	crm.BroadcastPath = str
	b = b[n:]

	str, n, err = ReadString(b)
	if err != nil {
		return err
	}
	crm.TrackName = str
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
