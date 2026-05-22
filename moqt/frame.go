package moqt

import (
	"io"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

// Frame represents a MOQ frame.
// It provides methods to build, read, and encode MOQ payloads.
type Frame struct {
	buf    []byte
	header [8]byte
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
	f.buf = make([]byte, 8+cap)
	body := f.buf[8:8]
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

// encode writes the frame in MOQ format: varint length followed by payload.
// The length is encoded into the header buffer to minimize allocations.
func (f *Frame) encode(w io.Writer) error {
	l := uint64(len(f.body))
	header, _ := message.WriteMessageLength(f.header[:0], l)
	start := 8 - len(header)
	copy(f.buf[start:], header)
	end := 8 + len(f.body)
	_, err := w.Write(f.buf[start:end])
	return err
}

// decode reads a MOQ frame from the reader, updating the payload.
// The payload buffer is reused or reallocated as needed.
func (f *Frame) decode(src io.Reader) error {
	num, err := message.ReadMessageLength(src)
	if err != nil {
		return err
	}

	// If payload length is zero, reset the slice to zero length
	if num == 0 {
		f.body = f.body[:0]
		return nil
	}

	// Ensure the payload slice has enough capacity
	if cap(f.body) < int(num) {
		f.buf = make([]byte, 8+num)
		f.body = f.buf[8 : 8+num]
	} else {
		f.body = f.body[:num]
	}

	_, err = io.ReadFull(src, f.body)

	return err
}

// Clone creates a deep copy of the frame, including all payload data.
// The cloned frame is completely independent from the original.
func (f *Frame) Clone() *Frame {
	clone := NewFrame(f.Cap())
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
