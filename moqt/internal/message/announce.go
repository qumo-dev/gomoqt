package message

import (
	"io"
)

const (
	ENDED  AnnounceStatus = 0x0
	ACTIVE AnnounceStatus = 0x1
	// LIVE   AnnounceStatus = 0x2
)

type AnnounceStatus byte

// AnnounceMessage is sent on an ANNOUNCE stream.
// Only the broadcast path suffix is carried on the wire; the receiver
// reconstructs the full broadcast path by prepending the requested prefix.
type AnnounceMessage struct {
	AnnounceStatus      AnnounceStatus
	BroadcastPathSuffix string
	Hops                uint64
}

func (am AnnounceMessage) Len() int {
	var l int

	l += VarintLen(uint64(am.AnnounceStatus))
	l += StringLen(am.BroadcastPathSuffix)
	l += VarintLen(am.Hops)

	return l
}

func (am AnnounceMessage) Encode(w io.Writer) error {
	msgLen := am.Len()

	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(am.AnnounceStatus))
	b, _ = WriteString(b, am.BroadcastPathSuffix)
	b, _ = WriteVarint(b, am.Hops)

	_, err := w.Write(b)

	return err
}

func (am *AnnounceMessage) Decode(src io.Reader) error {
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
	am.AnnounceStatus = AnnounceStatus(num)
	b = b[n:]

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	am.BroadcastPathSuffix = str
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	am.Hops = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
