package message

import (
	"io"
)

/*
* SUBSCRIBE Message {
*   Message Length (i)
*   Subscribe ID (i)
*   Broadcast Path (s)
*   Track Name (s)
*   Subscriber Priority (8)
*   Subscriber Max Age (i)
*   Group Start (i)
*   Group End (i)
*   Frame Start (i)
*   Frame End (i)
* }
*
* Group Start and Frame Start are plain absolute values (Group Start is a
* floor, 0 meaning none). Group End and Frame End are absolute + 1, with 0
* meaning unbounded / the whole group.
 */
type SubscribeMessage struct {
	SubscribeID        uint64
	BroadcastPath      string
	TrackName          string
	SubscriberPriority uint8
	SubscriberMaxAge   uint64
	GroupStart         uint64
	GroupEnd           uint64
	FrameStart         uint64
	FrameEnd           uint64
}

func (s SubscribeMessage) Len() int {
	var l int

	l += VarintLen(uint64(s.SubscribeID))
	l += StringLen(s.BroadcastPath)
	l += StringLen(s.TrackName)
	l += 1 // SubscriberPriority (uint8)
	l += VarintLen(s.SubscriberMaxAge)
	l += VarintLen(s.GroupStart)
	l += VarintLen(s.GroupEnd)
	l += VarintLen(s.FrameStart)
	l += VarintLen(s.FrameEnd)

	return l
}

func (s SubscribeMessage) Encode(w io.Writer) error {
	msgLen := s.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(s.SubscribeID))
	b, _ = WriteVarint(b, uint64(len(s.BroadcastPath)))
	b = append(b, s.BroadcastPath...)
	b, _ = WriteVarint(b, uint64(len(s.TrackName)))
	b = append(b, s.TrackName...)
	b = append(b, s.SubscriberPriority)
	b, _ = WriteVarint(b, s.SubscriberMaxAge)
	b, _ = WriteVarint(b, s.GroupStart)
	b, _ = WriteVarint(b, s.GroupEnd)
	b, _ = WriteVarint(b, s.FrameStart)
	b, _ = WriteVarint(b, s.FrameEnd)

	_, err := w.Write(b)
	return err
}

func (s *SubscribeMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	s.SubscribeID = num
	b = b[n:]

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	s.BroadcastPath = str
	b = b[n:]

	str, n, err = ReadString(b)
	if err != nil {
		return err
	}
	s.TrackName = str
	b = b[n:]

	if len(b) < 1 {
		return ErrMessageTooShort
	}
	s.SubscriberPriority = b[0]
	b = b[1:]

	for _, field := range [...]*uint64{&s.SubscriberMaxAge, &s.GroupStart, &s.GroupEnd, &s.FrameStart, &s.FrameEnd} {
		num, n, err = ReadVarint(b)
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
