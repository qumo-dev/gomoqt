package message

import "io"

/*
 *	FETCH Message {
 *	  Message Length (i)
 *	  Broadcast Path (s)
 *	  Track Name (s)
 *	  Subscriber Priority (8)
 *	  Group Sequence (i)
 *	  Frame Start (i)
 *	  Frame End (i)
 *	}
 *
 * Frame Start is a plain index; Frame End is the last index + 1, 0 meaning
 * through the end of the group.
 */
type FetchMessage struct {
	BroadcastPath string
	TrackName     string
	Priority      uint8
	GroupSequence uint64
	FrameStart    uint64
	FrameEnd      uint64
}

func (f FetchMessage) Len() int {
	var l int
	l += StringLen(f.BroadcastPath)
	l += StringLen(f.TrackName)
	l += 1 // Priority (uint8)
	l += VarintLen(f.GroupSequence)
	l += VarintLen(f.FrameStart)
	l += VarintLen(f.FrameEnd)
	return l
}

func (f FetchMessage) Encode(w io.Writer) error {
	msgLen := f.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))
	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(len(f.BroadcastPath)))
	b = append(b, f.BroadcastPath...)
	b, _ = WriteVarint(b, uint64(len(f.TrackName)))
	b = append(b, f.TrackName...)
	b = append(b, f.Priority)
	b, _ = WriteVarint(b, f.GroupSequence)
	b, _ = WriteVarint(b, f.FrameStart)
	b, _ = WriteVarint(b, f.FrameEnd)
	_, err := w.Write(b)
	return err
}

func (f *FetchMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	f.BroadcastPath = str
	b = b[n:]

	str, n, err = ReadString(b)
	if err != nil {
		return err
	}
	f.TrackName = str
	b = b[n:]

	if len(b) < 1 {
		return ErrMessageTooShort
	}
	f.Priority = b[0]
	b = b[1:]

	for _, field := range [...]*uint64{&f.GroupSequence, &f.FrameStart, &f.FrameEnd} {
		num, n, err := ReadVarint(b)
		if err != nil {
			return err
		}
		*field = num
		b = b[n:]
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
