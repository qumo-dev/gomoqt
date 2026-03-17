package moqt

import (
	"context"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

func newGroupWriter(stream transport.SendStream, sequence GroupSequence,
	onClose func()) *GroupWriter {

	return &GroupWriter{
		sequence: sequence,
		onClose:  onClose,
		stream:   stream,
		ctx:      context.WithValue(stream.Context(), &uniStreamTypeCtxKey, message.StreamTypeGroup),
	}
}

// GroupWriter writes frames for a single group.
// It manages the lifecycle of the group.
type GroupWriter struct {
	sequence GroupSequence

	ctx    context.Context
	stream transport.SendStream

	frameCount uint64 // Number of frames sent on this stream

	onClose func()
}

// GroupSequence returns the group sequence identifier associated with this writer.
func (sgs *GroupWriter) GroupSequence() GroupSequence {
	return sgs.sequence
}

// WriteFrame writes a Frame to the group stream.
func (sgs *GroupWriter) WriteFrame(frame *Frame) error {
	if frame == nil {
		return nil
	}

	err := frame.encode(sgs.stream)
	if err != nil {
		return err
	}

	sgs.frameCount++

	return nil
}

// SetWriteDeadline sets the write deadline for write operations.
func (sgs *GroupWriter) SetWriteDeadline(t time.Time) error {
	return sgs.stream.SetWriteDeadline(t)
}

// CancelWrite cancels the group with the specified GroupErrorCode and triggers callbacks.
func (sgs *GroupWriter) CancelWrite(code GroupErrorCode) {
	sgs.stream.CancelWrite(transport.StreamErrorCode(code))

	sgs.onClose()
}

// Close closes the group stream gracefully.
func (sgs *GroupWriter) Close() error {
	err := sgs.stream.Close()
	if err != nil {
		return Cause(sgs.ctx)
	}

	sgs.onClose()

	return nil
}

// Context returns the context associated with this writer.
func (s *GroupWriter) Context() context.Context {
	return s.ctx
}
