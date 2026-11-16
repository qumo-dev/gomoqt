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
	StreamTypeTrack     StreamType = 0x6
	StreamTypeAuth      StreamType = 0x7

	// Uni-directional Stream Types
	StreamTypeGroup StreamType = 0x0
	StreamTypeSetup StreamType = 0x1
)

// StreamType is the one-byte header that opens every stream and names what the
// stream carries. Bidirectional and unidirectional streams number their types
// separately, so the same value means different things on each.
type StreamType byte

// Encode writes a one-byte stream type header.
func (stm StreamType) Encode(w io.Writer) error {
	_, err := w.Write([]byte{byte(stm)})
	return err
}

// Decode reads the one-byte stream type. It uses io.ReadFull rather than a bare
// Read: a transport may return the byte together with io.EOF, or return no
// bytes and no error, and both are valid io.Reader behavior.
func (stm *StreamType) Decode(r io.Reader) error {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	*stm = StreamType(buf[0])

	return nil
}
