package message

import (
	"io"
)

/*
* SUBSCRIBE Message {
*   Message Length (varint)
*   Subscribe ID (varint)
*   Broadcast Path (string)
*   Track Name (string)
*   Subscriber Priority (varint)
*   Subscriber Ordered (varint)
*   Subscriber Max Latency (varint)
*   Start Group (varint)
*   End Group (varint)
* }
*
* Broadcast Path and Track Name are length-prefixed UTF-8 strings.
* Start Group and End Group use 0 for the default/latest and unbounded values.
 */
type SubscribeMessage struct {
	SubscribeID          uint64
	BroadcastPath        string
	TrackName            string
	SubscriberPriority   uint8
	SubscriberOrdered    uint8
	SubscriberMaxLatency uint64
	StartGroup           uint64
	EndGroup             uint64
}

func (s SubscribeMessage) Len() int {
	var l int

	l += VarintLen(uint64(s.SubscribeID))
	l += StringLen(s.BroadcastPath)
	l += StringLen(s.TrackName)
	l += VarintLen(uint64(s.SubscriberPriority))
	l += VarintLen(uint64(s.SubscriberOrdered))
	l += VarintLen(s.SubscriberMaxLatency)
	l += VarintLen(s.StartGroup)
	l += VarintLen(s.EndGroup)

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
	b, _ = WriteVarint(b, uint64(s.SubscriberPriority))
	b, _ = WriteVarint(b, uint64(s.SubscriberOrdered))
	b, _ = WriteVarint(b, s.SubscriberMaxLatency)
	b, _ = WriteVarint(b, s.StartGroup)
	b, _ = WriteVarint(b, s.EndGroup)

	_, err := w.Write(b)
	return err
}

func (s *SubscribeMessage) Decode(src io.Reader) error {
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

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	s.SubscriberPriority = uint8(num)
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	s.SubscriberOrdered = uint8(num)
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	s.SubscriberMaxLatency = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	s.StartGroup = num
	b = b[n:]

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	s.EndGroup = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
