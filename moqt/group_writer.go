package moqt

import (
	"context"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

func newGroupWriter(stream transport.SendStream, sequence GroupSequence, groupManager *groupWriterManager) *GroupWriter {
	w := &GroupWriter{
		sequence:     sequence,
		groupManager: groupManager,
		stream:       stream,
		ctx:          context.WithValue(stream.Context(), uniStreamTypeCtxKey, message.StreamTypeGroup),
	}

	if w.groupManager != nil {
		w.groupManager.addGroup(w)
	}

	return w
}

// GroupWriter writes frames for a single group.
// It manages the lifecycle of the group.
type GroupWriter struct {
	sequence GroupSequence

	ctx    context.Context
	stream transport.SendStream

	frameCount uint64 // Number of frames sent on this stream

	// prevTimestamp is the previous frame's timestamp on this stream,
	// used to delta-encode the next frame (the first frame is encoded
	// relative to 0).
	prevTimestamp uint64

	groupManager *groupWriterManager
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

	err := frame.encode(sgs.stream, sgs.prevTimestamp)
	if err != nil {
		return err
	}

	sgs.prevTimestamp = frame.Timestamp
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

	if sgs.groupManager != nil {
		sgs.groupManager.removeGroup(sgs)
	}
}

// Close closes the group stream gracefully.
func (sgs *GroupWriter) Close() error {
	err := sgs.stream.Close()
	if err != nil {
		return Cause(sgs.ctx)
	}

	if sgs.groupManager != nil {
		sgs.groupManager.removeGroup(sgs)
	}

	return nil
}

// Context returns the context associated with this writer.
func (s *GroupWriter) Context() context.Context {
	return s.ctx
}
