package message

import (
	"fmt"
	"io"
)

// MaxNewSessionURILen is the longest GOAWAY New Session URI, in bytes
// (moq-lite-06 §7.18, matching moq-transport).
const MaxNewSessionURILen = 8192

// ErrNewSessionURITooLong is returned for a GOAWAY whose New Session URI
// exceeds MaxNewSessionURILen. Receiving one is a protocol violation.
// It wraps ErrMessageTooLarge.
var ErrNewSessionURITooLong = fmt.Errorf("GOAWAY new session URI too long: %w", ErrMessageTooLarge)

/*
 *	GOAWAY Message {
 *	  Message Length (i)
 *	  New Session URI (s)
 *	}
 */
type GoawayMessage struct {
	NewSessionURI string
}

func (gm GoawayMessage) Len() int {
	return StringLen(gm.NewSessionURI)
}

// Encode writes the message. It refuses a URI longer than
// MaxNewSessionURILen, which the peer would treat as a protocol violation.
func (gm GoawayMessage) Encode(w io.Writer) error {
	if len(gm.NewSessionURI) > MaxNewSessionURILen {
		return ErrNewSessionURITooLong
	}
	msgLen := gm.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, gm.NewSessionURI)

	_, err := w.Write(b)

	return err
}

// Decode reads the message. A URI longer than MaxNewSessionURILen is
// rejected from the length prefix alone, before its bytes are read.
func (gm *GoawayMessage) Decode(src io.Reader) error {
	num, err := ReadMessageLength(src)
	if err != nil {
		return err
	}
	// The URI's length varint may use any of its encodings, up to 8 bytes,
	// so the body bound allows the longest; the URI itself is checked below.
	if num > MaxNewSessionURILen+8 {
		return ErrNewSessionURITooLong
	}

	b := make([]byte, num)
	if _, err := io.ReadFull(src, b); err != nil {
		return err
	}

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	if len(str) > MaxNewSessionURILen {
		return ErrNewSessionURITooLong
	}
	gm.NewSessionURI = str
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
