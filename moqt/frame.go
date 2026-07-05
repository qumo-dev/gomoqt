package moqt

import (
	"io"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

// frameHeaderSize reserves room in the frame buffer for the wire prefix:
// a Timestamp Delta varint (up to 8 bytes) followed by a Message Length
// varint (up to 8 bytes).
const frameHeaderSize = 16

// Frame represents a MOQ frame.
// It provides methods to build, read, and encode MOQ payloads.
type Frame struct {
	// Timestamp is the frame's presentation timestamp, expressed in the
	// track's Timescale (units per second, carried in TRACK_INFO).
	// It is encoded on the wire as a zigzag delta from the previous frame
	// on the same group stream; the first frame is delta-encoded from 0.
	Timestamp uint64

	buf    []byte
	header [frameHeaderSize]byte
	body   []byte
}

// NewFrame creates a new Frame with the specified payload capacity.
// The frame is initialized with empty payload and ready for data to be appended.
func NewFrame(cap int) *Frame {
	f := &Frame{}
	f.init(cap)
	return f
}

// Reset clears the frame payload while preserving the buffer capacity.
// This allows the frame to be reused without reallocation.
func (f *Frame) Reset() {
	f.body = f.body[:0]
}

// Body returns the frame payload bytes.
// Use Write to add data and Reset to clear the frame.
func (f *Frame) Body() []byte {
	return f.body
}

func (f *Frame) init(cap int) {
	f.buf = make([]byte, frameHeaderSize+cap)
	body := f.buf[frameHeaderSize:frameHeaderSize]
	if f.body != nil {
		body = body[:len(f.body)]
		copy(body, f.body)
	}
	f.body = body
}

// append appends bytes to the frame payload and grows the buffer when needed.
// This helper is used by Write and Clone.
func (f *Frame) append(b []byte) {
	if len(b)+len(f.body) > cap(f.body) {
		// Reallocate the body buffer if necessary
		cap := max(len(f.body)+len(b), 2*cap(f.body))
		f.init(cap)
	}

	f.body = append(f.body, b...)
}

// Len returns the current length of the payload in bytes.
func (f *Frame) Len() int {
	return len(f.body)
}

// Cap returns the current capacity of the payload buffer.
func (f *Frame) Cap() int {
	return cap(f.body)
}

// encode writes the frame in MOQ format: zigzag timestamp delta relative to
// prevTimestamp, varint length, then payload. The prefix is encoded into the
// header area of the frame buffer to minimize allocations and writes.
func (f *Frame) encode(w io.Writer, prevTimestamp uint64) error {
	delta := message.ZigzagEncode(int64(f.Timestamp) - int64(prevTimestamp))
	l := uint64(len(f.body))

	prefix, _ := message.WriteVarint(f.header[:0], delta)
	prefix, _ = message.WriteMessageLength(prefix, l)

	start := frameHeaderSize - len(prefix)
	copy(f.buf[start:], prefix)
	end := frameHeaderSize + len(f.body)
	_, err := w.Write(f.buf[start:end])
	return err
}

// decode reads a MOQ frame from the reader, updating the timestamp and payload.
// prevTimestamp is the previous frame's timestamp on the same stream (0 for the
// first frame). The payload buffer is reused or reallocated as needed.
func (f *Frame) decode(src io.Reader, prevTimestamp uint64) error {
	delta, err := message.ReadMessageLength(src)
	if err != nil {
		return err
	}
	f.Timestamp = uint64(int64(prevTimestamp) + message.ZigzagDecode(delta))

	num, err := message.ReadMessageLength(src)
	if err != nil {
		return err
	}

	// If payload length is zero, reset the slice to zero length
	if num == 0 {
		f.body = f.body[:0]
		return nil
	}

	// Cap the allocation derived from the untrusted length prefix to prevent an
	// OOM DoS: a peer can advertise a maxUint62 payload length and force a
	// multi-GB buffer allocation before any payload bytes are read.
	if num > message.MaxMessageSize {
		return message.ErrMessageTooLarge
	}

	// Ensure the payload slice has enough capacity
	if cap(f.body) < int(num) {
		// Use init to grow the underlying buffer exponentially,
		// maintaining the frame invariant that f.body is a subslice of f.buf.
		f.init(max(int(num), 2*cap(f.body)))
	}
	f.body = f.body[:num]

	_, err = io.ReadFull(src, f.body)

	return err
}

// Clone creates a deep copy of the frame, including all payload data.
// The cloned frame is completely independent from the original.
func (f *Frame) Clone() *Frame {
	clone := NewFrame(f.Cap())
	clone.Timestamp = f.Timestamp
	clone.append(f.Body())
	return clone
}

// WriteTo writes the payload to the writer, returning the number of bytes written.
func (f *Frame) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.body)
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

// Write implements io.Writer interface for frame payloads.
// It appends the provided bytes to the frame and returns the number of bytes written.
func (f *Frame) Write(p []byte) (int, error) {
	f.append(p)
	return len(p), nil
}
