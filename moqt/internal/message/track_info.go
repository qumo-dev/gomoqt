package message

import (
	"io"
)

/*
 *	TRACK_INFO Message {
 *	  Message Length (i)
 *	  Publisher Priority (8)
 *	  Publisher Max Age (i)
 *	  Timescale (i)
 *	}
 *
 * Every field is fixed for the lifetime of the Track.
 * Timescale MUST be non-zero.
 */
type TrackInfoMessage struct {
	PublisherPriority uint8
	PublisherMaxAge   uint64
	Timescale         uint64
}

func (tim TrackInfoMessage) Len() int {
	var l int

	l += 1 // PublisherPriority (uint8)
	l += VarintLen(tim.PublisherMaxAge)
	l += VarintLen(tim.Timescale)

	return l
}

func (tim TrackInfoMessage) Encode(w io.Writer) error {
	msgLen := tim.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b = append(b, tim.PublisherPriority)
	b, _ = WriteVarint(b, tim.PublisherMaxAge)
	b, _ = WriteVarint(b, tim.Timescale)

	_, err := w.Write(b)

	return err
}

func (tim *TrackInfoMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	if len(b) < 1 {
		return ErrMessageTooShort
	}
	tim.PublisherPriority = b[0]
	b = b[1:]

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	tim.PublisherMaxAge = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	tim.Timescale = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
