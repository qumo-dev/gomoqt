package message

import (
	"io"
)

const (
	/*
	 * Bidirectional Stream Type
	 */
	// StreamTypeSession is deprecated in moq-lite-03 and kept only for
	// transition compatibility.
	StreamTypeSession   StreamType = 0x0
	StreamTypeAnnounce  StreamType = 0x1
	StreamTypeSubscribe StreamType = 0x2
	StreamTypeFetch     StreamType = 0x3
	StreamTypeProbe     StreamType = 0x4

	/*
	 * Unidirectional Stream Type
	 */
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
