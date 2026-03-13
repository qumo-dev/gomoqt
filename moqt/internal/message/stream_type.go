package message

import (
	"io"
)

const (
	/*
	 * Bidirectional Stream Type
	 */
	// StreamTypeSession is deprecated in moq-lite-03 and kept for transition.
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

/*
 * Serialize the message in the following format
 *
 * STREAM_TYPE Message {
 *   Stream Type (byte),
 * }
 */

func (stm StreamType) Encode(w io.Writer) error {
	// Write the Stream Type
	_, err := w.Write([]byte{byte(stm)})
	return err
}

func (stm *StreamType) Decode(r io.Reader) error {
	// Read the Stream Type
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return err
	}
	*stm = StreamType(buf[0])

	return nil
}
