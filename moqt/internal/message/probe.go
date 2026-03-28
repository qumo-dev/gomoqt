package message

import (
	"io"
)

// ProbeMessage is sent on the Probe stream (0x4).
// The subscriber sends a target bitrate; the publisher replies with the
// measured bitrate.
type ProbeMessage struct {
	// Bitrate is the target or measured bitrate in bits per second.
	Bitrate uint64
}

func (pm ProbeMessage) Len() int {
	return VarintLen(pm.Bitrate)
}

func (pm ProbeMessage) Encode(w io.Writer) error {
	msgLen := pm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, pm.Bitrate)

	_, err := w.Write(b)
	return err
}

func (pm *ProbeMessage) Decode(src io.Reader) error {
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
	pm.Bitrate = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
