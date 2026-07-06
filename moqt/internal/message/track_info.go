package message

import (
	"io"
)

/*
 *	TRACK_INFO Message {
 *	  Message Length (i)
 *	  Publisher Priority (8)
 *	  Publisher Ordered (8)
 *	  Publisher Max Latency (i)
 *	  Timescale (i)
 *	}
 *
 * Every field is fixed for the lifetime of the Track.
 * Timescale MUST be non-zero.
 */
type TrackInfoMessage struct {
	PublisherPriority   uint8
	PublisherOrdered    uint8
	PublisherMaxLatency uint64
	Timescale           uint64
}

func (tim TrackInfoMessage) Len() int {
	var l int

	l += 1 // PublisherPriority (uint8)
	l += 1 // PublisherOrdered (uint8)
	l += VarintLen(tim.PublisherMaxLatency)
	l += VarintLen(tim.Timescale)

	return l
}

func (tim TrackInfoMessage) Encode(w io.Writer) error {
	msgLen := tim.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b = append(b, tim.PublisherPriority)
	b = append(b, tim.PublisherOrdered)
	b, _ = WriteVarint(b, tim.PublisherMaxLatency)
	b, _ = WriteVarint(b, tim.Timescale)

	_, err := w.Write(b)

	return err
}

func (tim *TrackInfoMessage) Decode(src io.Reader) error {
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

	if len(b) < 2 {
		return ErrMessageTooShort
	}
	tim.PublisherPriority = b[0]
	tim.PublisherOrdered = b[1]
	b = b[2:]

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	tim.PublisherMaxLatency = num
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
