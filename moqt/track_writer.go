package moqt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

type groupWriterManager struct {
	mu           sync.Mutex
	activeGroups map[*GroupWriter]struct{}

	closed bool
}

func newGroupWriterManager() *groupWriterManager {
	return &groupWriterManager{
		activeGroups: make(map[*GroupWriter]struct{}),
	}
}

func (m *groupWriterManager) addGroup(group *GroupWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.activeGroups[group] = struct{}{}
}

func (m *groupWriterManager) removeGroup(group *GroupWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.activeGroups, group)
}

func (m *groupWriterManager) countGroups() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.activeGroups)
}

func (m *groupWriterManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.activeGroups = nil
}

func newTrackWriter(
	broadcastPath BroadcastPath,
	trackName TrackName,
	subscribeStream *receiveSubscribeStream,
	openUniStreamFunc func() (transport.SendStream, error),
	onCloseTrackFunc func(),
) *TrackWriter {
	streamCtx := subscribeStream.stream.Context()

	track := &TrackWriter{
		BroadcastPath:     broadcastPath,
		TrackName:         trackName,
		subscribeStream:   subscribeStream,
		groupManager:      newGroupWriterManager(),
		openUniStreamFunc: openUniStreamFunc,
		onCloseTrackFunc:  onCloseTrackFunc,
		ctx:               context.WithValue(streamCtx, biStreamTypeCtxKey, message.StreamTypeSubscribe),
	}

	return track
}

// TrackWriter writes groups for a published track.
// It manages the lifecycle of active groups for that track.
// The TrackWriter provides output-side methods to send track data.
type TrackWriter struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName

	subscribeStream *receiveSubscribeStream

	groupManager *groupWriterManager

	mu sync.RWMutex

	// groupSequence is atomically incremented for each OpenGroup call
	groupSequence atomic.Uint64

	openUniStreamFunc func() (transport.SendStream, error)

	onCloseTrackFunc func()

	ctx context.Context
}

// Close stops publishing and cancels active groups.
func (w *TrackWriter) Close() error {
	// Take the write lock to ensure Close is exclusive with OpenGroup calls.
	// This prevents OpenGroup from running concurrently with Close and
	// provides a deterministic semantics: either the OpenGroup completes
	// entirely before Close proceeds, or Close waits for OpenGroup to finish.
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.groupManager != nil {
		groupManager := w.groupManager
		w.groupManager = nil

		activeGroups := groupManager.activeGroups

		groupManager.close()

		for g := range activeGroups {
			_ = g.Close()
		}
	}

	if w.onCloseTrackFunc != nil {
		w.onCloseTrackFunc()
		w.onCloseTrackFunc = nil
	}

	if w.subscribeStream == nil {
		return nil
	}

	return w.subscribeStream.close()
}

// CloseWithError stops publishing due to an error and cancels active groups.
func (w *TrackWriter) CloseWithError(code SubscribeErrorCode) {
	// Ensure CloseWithError is exclusive with OpenGroup.
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.groupManager != nil {
		groupManager := w.groupManager
		w.groupManager = nil

		activeGroups := groupManager.activeGroups

		groupManager.close()

		for g := range activeGroups {
			g.CancelWrite(PublishAbortedErrorCode)
		}
	}

	if w.onCloseTrackFunc != nil {
		w.onCloseTrackFunc()
		w.onCloseTrackFunc = nil
	}

	if w.subscribeStream != nil {
		w.subscribeStream.closeWithError(code)
	}
}

// OpenGroup opens a new group with an automatically incremented sequence number
// and returns a GroupWriter to write frames into it.
// The sequence starts at 1 and increments by 1 for each call.
func (w *TrackWriter) OpenGroup() (*GroupWriter, error) {
	// Atomically increment and get the next sequence
	seq := GroupSequence(w.groupSequence.Add(1))
	return w.openGroupWithSequence(seq)
}

// OpenGroupAt opens a new group with the specified sequence number.
// It advances the internal next-sequence counter to at least seq+1 so that
// subsequent OpenGroup calls will not produce a duplicate sequence.
func (w *TrackWriter) OpenGroupAt(seq GroupSequence) (*GroupWriter, error) {
	// Advance the internal counter to avoid collisions with subsequent
	// OpenGroup calls. CAS loop ensures correctness under concurrency.
	for {
		cur := w.groupSequence.Load()
		next := max(cur, uint64(seq)+1)
		if next == cur {
			break
		}
		if w.groupSequence.CompareAndSwap(cur, next) {
			break
		}
	}
	return w.openGroupWithSequence(seq)
}

// SkipGroups skips the next n group sequences without opening them.
// This is useful when you need to intentionally create gaps in the sequence,
// for example, when dropping groups due to packet loss or priority decisions.
func (w *TrackWriter) SkipGroups(n uint64) {
	w.groupSequence.Add(n)
}

func (w *TrackWriter) Context() context.Context {
	return w.ctx
}

func (w *TrackWriter) WriteInfo(info PublishInfo) error {
	return w.subscribeStream.writeInfo(info)
}

// DropGroups sends a SUBSCRIBE_DROP message for an explicit inclusive range.
// The range is expressed in absolute group sequence numbers.
func (w *TrackWriter) DropGroups(drop SubscribeDrop) error {
	if drop.StartGroup == MinGroupSequence || drop.EndGroup == MinGroupSequence {
		return fmt.Errorf("invalid drop range: start=%d end=%d", drop.StartGroup, drop.EndGroup)
	}
	if drop.StartGroup > drop.EndGroup {
		return fmt.Errorf("invalid drop range: start=%d end=%d", drop.StartGroup, drop.EndGroup)
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.Context().Err() != nil {
		return Cause(w.Context())
	}

	if w.subscribeStream == nil {
		return fmt.Errorf("track writer is closed")
	}

	return w.subscribeStream.writeDrop(drop)
}

// DropNextGroups skips the next n groups and emits a SUBSCRIBE_DROP for the
// skipped inclusive range.
func (w *TrackWriter) DropNextGroups(n uint64, code SubscribeErrorCode) error {
	if n == 0 {
		return nil
	}

	for {
		cur := w.groupSequence.Load()
		start := GroupSequence(cur + 1)
		end := GroupSequence(cur + n)

		if end < start {
			return fmt.Errorf("drop range overflow: start=%d end=%d", start, end)
		}

		if w.groupSequence.CompareAndSwap(cur, uint64(end)) {
			return w.DropGroups(SubscribeDrop{
				StartGroup: start,
				EndGroup:   end,
				ErrorCode:  code,
			})
		}
	}
}

func (w *TrackWriter) TrackConfig() *SubscribeConfig {
	if w.subscribeStream == nil {
		return &SubscribeConfig{}
	}

	return w.subscribeStream.TrackConfig()
}

func (w *TrackWriter) Updated() <-chan struct{} {
	return w.subscribeStream.Updated()
}

// openGroupWithSequence is the internal implementation for opening a group with a specific sequence.
func (w *TrackWriter) openGroupWithSequence(seq GroupSequence) (*GroupWriter, error) {
	// Avoid accessing s.ctx directly; it can be nil if the receiveSubscribeStream
	// has been cleared during Close(). Instead, capture the receiveSubscribeStream
	// under lock and validate its context below.

	// Prevent opening a new group if the track has been closed. Capture
	// receiveSubscribeStream under lock so it cannot be set to nil by Close()
	// Acquire a shared lock so multiple OpenGroup calls can proceed
	// concurrently while ensuring Close waits for them to finish.
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Check the context on the captured receiveSubscribeStream instead of s.ctx
	// to avoid nil deref if the embedded field has been cleared by Close().
	if w.Context().Err() != nil {
		return nil, Cause(w.Context())
	}

	// Write the INFO message to the receive subscribe stream.
	err := w.WriteInfo(PublishInfo{
		StartGroup: seq,
	})
	if err != nil {
		return nil, err
	}

	stream, err := w.openUniStreamFunc()
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			sessErr := &SessionError{
				ApplicationError: appErr,
			}
			return nil, sessErr
		}
		return nil, err
	}

	err = message.StreamTypeGroup.Encode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := transport.StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	err = message.GroupMessage{
		SubscribeID:   uint64(w.subscribeStream.subscribeID),
		GroupSequence: uint64(seq),
	}.Encode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := transport.StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	return newGroupWriter(stream, seq, w.groupManager), nil
}
