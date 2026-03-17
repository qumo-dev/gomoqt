package moqt

import (
	"errors"
	"io"
	"iter"
	"time"

	"github.com/okdaichi/gomoqt/transport"
)

func newGroupReader(sequence GroupSequence, stream transport.ReceiveStream,
	onClose func()) *GroupReader {
	return &GroupReader{
		sequence: sequence,
		stream:   stream,
		// frame:    newFrame(0),
		onClose: onClose,
	}
}

// GroupReader receives group data for a subscribed track.
// Each GroupReader corresponds to a GroupSequence and provides methods to read frames.
type GroupReader struct {
	sequence GroupSequence

	stream     transport.ReceiveStream
	frameCount int64

	onClose func()
}

// GroupSequence returns the GroupSequence this reader belongs to.
func (s *GroupReader) GroupSequence() GroupSequence {
	return s.sequence
}

// ReadFrame decodes the next Frame from the group stream into the provided frame buffer.
// If io.EOF is returned, the group stream has been closed.
func (s *GroupReader) ReadFrame(frame *Frame) error {
	if frame == nil {
		panic("nil frame")
	}
	err := frame.decode(s.stream)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}

		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			grpErr := &GroupError{
				StreamError: strErr,
			}

			return grpErr
		}

		return err
	}

	s.frameCount++

	return nil
}

// CancelRead cancels the group using the provided GroupErrorCode.
func (s *GroupReader) CancelRead(code GroupErrorCode) {
	strErrCode := transport.StreamErrorCode(code)
	s.stream.CancelRead(strErrCode)
}

// SetReadDeadline sets the read deadline for read operations.
func (s *GroupReader) SetReadDeadline(t time.Time) error {
	return s.stream.SetReadDeadline(t)
}

// Frames returns a sequence that yields decoded frames from the group stream.
func (s *GroupReader) Frames(buf *Frame) iter.Seq[*Frame] {
	return func(yield func(*Frame) bool) {
		if buf == nil {
			buf = NewFrame(0)
		}
		var err error
		for {
			err = s.ReadFrame(buf)
			if err != nil {
				return
			}

			if !yield(buf) {
				return
			}
		}
	}
}
