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
	HopIDs              []uint64
}

func (am AnnounceMessage) Len() int {
	var l int

	l += VarintLen(uint64(am.AnnounceStatus))
	l += StringLen(am.BroadcastPathSuffix)
	l += VarintLen(uint64(len(am.HopIDs)))
	for _, id := range am.HopIDs {
		l += VarintLen(id)
	}

	return l
}

func (am AnnounceMessage) Encode(w io.Writer) error {
	msgLen := am.Len()

	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(am.AnnounceStatus))
	b, _ = WriteString(b, am.BroadcastPathSuffix)
	b, _ = WriteVarint(b, uint64(len(am.HopIDs)))
	for _, id := range am.HopIDs {
		b, _ = WriteVarint(b, id)
	}

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

	hopCount, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	b = b[n:]

	am.HopIDs = make([]uint64, hopCount)
	for i := range hopCount {
		num, n, err = ReadVarint(b)
		if err != nil {
			return err
		}
		am.HopIDs[i] = num
		b = b[n:]
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
