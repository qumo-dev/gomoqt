package message

import "io"

type FetchMessage struct {
	BroadcastPath string
	TrackName     string
	Priority      uint8
	GroupSequence uint64
}

func (f FetchMessage) Len() int {
	var l int
	l += StringLen(f.BroadcastPath)
	l += StringLen(f.TrackName)
	l += 1 // Priority (uint8)
	l += VarintLen(f.GroupSequence)
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
	_, err := w.Write(b)
	return err
}

func (f *FetchMessage) Decode(src io.Reader) error {
	size, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if size > MaxMessageAllocationSize {
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

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	f.GroupSequence = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
