package moqt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
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
	openUniStreamFunc func() (SendStream, error),
	onCloseTrackFunc func(),
) *TrackWriter {
	track := &TrackWriter{
		BroadcastPath:          broadcastPath,
		TrackName:              trackName,
		receiveSubscribeStream: subscribeStream,
		groupManager:           newGroupWriterManager(),
		openUniStreamFunc:      openUniStreamFunc,
		onCloseTrackFunc:       onCloseTrackFunc,
	}

	return track
}

// TrackWriter writes groups for a published track.
// It manages the lifecycle of active groups for that track.
// The TrackWriter provides output-side methods to send track data.
type TrackWriter struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName

	groupManager *groupWriterManager

	receiveSubscribeStream *receiveSubscribeStream

	// closeMu controls exclusivity between Close/CloseWithError and OpenGroup.
	// OpenGroup acquires a read lock so multiple OpenGroup calls may run
	// concurrently, while Close/CloseWithError acquires a write lock to
	// make the close exclusive with ongoing and future OpenGroup operations
	// until close completes.
	closeMu sync.RWMutex

	// groupSequence is atomically incremented for each OpenGroup call
	groupSequence atomic.Uint64

	openUniStreamFunc func() (SendStream, error)

	onCloseTrackFunc func()
}

// Close stops publishing and cancels active groups.
func (w *TrackWriter) Close() error {
	// Take the write lock to ensure Close is exclusive with OpenGroup calls.
	// This prevents OpenGroup from running concurrently with Close and
	// provides a deterministic semantics: either the OpenGroup completes
	// entirely before Close proceeds, or Close waits for OpenGroup to finish.
	w.closeMu.Lock()
	defer w.closeMu.Unlock()

	if w.groupManager != nil {
		groupManager := w.groupManager
		w.groupManager = nil

		activeGroups := groupManager.activeGroups

		groupManager.close()

		for g := range activeGroups {
			_ = g.Close()
		}
	}

	// Then close the subscribe stream if present
	var err error
	if w.receiveSubscribeStream != nil {
		err = w.receiveSubscribeStream.close()
		w.receiveSubscribeStream = nil
	}

	if w.onCloseTrackFunc != nil {
		w.onCloseTrackFunc()
		w.onCloseTrackFunc = nil
	}

	return err
}

// CloseWithError stops publishing due to an error and cancels active groups.
func (w *TrackWriter) CloseWithError(code SubscribeErrorCode) {
	// Ensure CloseWithError is exclusive with OpenGroup.
	w.closeMu.Lock()
	defer w.closeMu.Unlock()

	if w.groupManager != nil {
		groupManager := w.groupManager
		w.groupManager = nil

		activeGroups := groupManager.activeGroups

		groupManager.close()

		for g := range activeGroups {
			g.CancelWrite(PublishAbortedErrorCode)
		}
	}

	if w.receiveSubscribeStream != nil {
		_ = w.receiveSubscribeStream.closeWithError(code)
		w.receiveSubscribeStream = nil
	}

	if w.onCloseTrackFunc != nil {
		w.onCloseTrackFunc()
		w.onCloseTrackFunc = nil
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
	return w.receiveSubscribeStream.Context()
}

func (w *TrackWriter) WriteInfo(info PublishInfo) error {
	return w.receiveSubscribeStream.writeInfo(info)
}

func (w *TrackWriter) TrackConfig() *SubscribeConfig {
	if w.receiveSubscribeStream == nil {
		return &SubscribeConfig{}
	}
	return w.receiveSubscribeStream.TrackConfig()
}

func (w *TrackWriter) Updated() <-chan struct{} {
	if w.receiveSubscribeStream == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return w.receiveSubscribeStream.Updated()
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
	w.closeMu.RLock()
	defer w.closeMu.RUnlock()

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
		var appErr *ApplicationError
		if errors.As(err, &appErr) {
			sessErr := &SessionError{
				ApplicationError: appErr,
			}
			return nil, sessErr
		}
		return nil, err
	}

	err = message.StreamTypeGroup.Encode(stream)
	if err != nil {
		var strErr *StreamError
		if errors.As(err, &strErr) {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	err = message.GroupMessage{
		SubscribeID:   uint64(w.receiveSubscribeStream.subscribeID),
		GroupSequence: uint64(seq),
	}.Encode(stream)
	if err != nil {
		var strErr *StreamError
		if errors.As(err, &strErr) {
			return nil, &GroupError{StreamError: strErr}
		}

		strErrCode := StreamErrorCode(InternalGroupErrorCode)
		stream.CancelWrite(strErrCode)

		return nil, err
	}

	return newGroupWriter(stream, seq, w.groupManager), nil
}
