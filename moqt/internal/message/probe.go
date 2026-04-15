package message

import (
	"io"
)

// ProbeMessage is sent on the Probe stream (0x4).
// The subscriber sends a target bitrate; the publisher replies with the
// measured bitrate and RTT.
type ProbeMessage struct {
	// Bitrate is the target or measured bitrate in bits per second.
	// A value of 0 means unknown.
	Bitrate uint64
	// RTT is the smoothed round-trip time in milliseconds.
	// A value of 0 means unknown.
	RTT uint64
}

func (pm ProbeMessage) Len() int {
	return VarintLen(pm.Bitrate) + VarintLen(pm.RTT)
}

func (pm ProbeMessage) Encode(w io.Writer) error {
	msgLen := pm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, pm.Bitrate)
	b, _ = WriteVarint(b, pm.RTT)

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

	num, n, err = ReadVarint(b)
	if err != nil {
		return err
	}
	pm.RTT = num
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
