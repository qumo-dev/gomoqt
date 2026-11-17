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
//
// Frames are numbered from 0 within the group, in the order WriteFrame is
// called. When the subscription (or fetch) asked for only part of the group,
// the frames outside that range are counted but not sent: write the group
// whole and the writer sends the part that was asked for.
type GroupWriter struct {
	sequence GroupSequence

	ctx    context.Context
	stream transport.SendStream

	// frameIndex is the index within the group of the next frame written.
	frameIndex uint64
	// frameStart is the index of the first frame sent, and frameEnd the
	// exclusive end, 0 meaning the end of the group.
	frameStart uint64
	frameEnd   uint64

	// prevTimestamp is the previous frame's timestamp on this stream,
	// used to delta-encode the next frame (the first frame is encoded
	// relative to 0).
	prevTimestamp uint64

	// ages, when set, tracks the timestamps of the track's groups to expire
	// the ones the subscriber's Max Age leaves behind.
	ages *groupAges

	groupManager *groupWriterManager
}

// GroupSequence returns the group sequence identifier associated with this writer.
func (sgs *GroupWriter) GroupSequence() GroupSequence {
	return sgs.sequence
}

// WriteFrame writes a Frame to the group stream. A frame outside the range
// the subscriber asked for is skipped and nil is returned.
func (sgs *GroupWriter) WriteFrame(frame *Frame) error {
	if frame == nil {
		return nil
	}

	if sgs.ages != nil {
		sgs.ages.observe(sgs, frame.Timestamp)
	}

	idx := sgs.frameIndex
	if idx < sgs.frameStart || (sgs.frameEnd != 0 && idx >= sgs.frameEnd) {
		sgs.frameIndex++
		return nil
	}

	err := frame.encode(sgs.stream, sgs.prevTimestamp)
	if err != nil {
		return err
	}

	sgs.prevTimestamp = frame.Timestamp
	sgs.frameIndex++

	return nil
}

// SetWriteDeadline sets the write deadline for write operations.
func (sgs *GroupWriter) SetWriteDeadline(t time.Time) error {
	return sgs.stream.SetWriteDeadline(t)
}

// CancelWrite cancels the group with the specified GroupErrorCode and triggers callbacks.
func (sgs *GroupWriter) CancelWrite(code GroupErrorCode) {
	sgs.stream.CancelWrite(transport.StreamErrorCode(code))
	sgs.release()
}

// Close closes the group stream gracefully.
func (sgs *GroupWriter) Close() error {
	err := sgs.stream.Close()
	// The stream has ended either way: release it even when Close failed, or
	// the group's Max Age tracking would hold it as open, keeping every later
	// closed group from being pruned.
	sgs.release()
	if err != nil {
		return Cause(sgs.ctx)
	}

	return nil
}

// release unregisters the writer once its stream has ended.
func (sgs *GroupWriter) release() {
	if sgs.groupManager != nil {
		sgs.groupManager.removeGroup(sgs)
	}
	if sgs.ages != nil {
		sgs.ages.closed(sgs)
	}
}

// Context returns the context associated with this writer.
func (s *GroupWriter) Context() context.Context {
	return s.ctx
}
