package message

import (
	"io"
)

const (
	// Bi-directional Stream Types
	StreamTypeAnnounce  StreamType = 0x1
	StreamTypeSubscribe StreamType = 0x2
	StreamTypeFetch     StreamType = 0x3
	StreamTypeProbe     StreamType = 0x4
	StreamTypeGoaway    StreamType = 0x5

	// Uni-directional Stream Types
	StreamTypeGroup StreamType = 0x0
)

type StreamType byte

// Encode writes a one-byte stream type header.
func (stm StreamType) Encode(w io.Writer) error {
	_, err := w.Write([]byte{byte(stm)})
	return err
}

func (stm *StreamType) Decode(r io.Reader) error {
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return err
	}
	*stm = StreamType(buf[0])

	return nil
}
