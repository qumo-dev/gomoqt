package moqt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
)

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
		activeGroups:           make(map[*GroupWriter]struct{}),
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

	receiveSubscribeStream *receiveSubscribeStream

	groupMapMu   sync.Mutex
	activeGroups map[*GroupWriter]struct{}

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
func (s *TrackWriter) Close() error {
	// Take the write lock to ensure Close is exclusive with OpenGroup calls.
	// This prevents OpenGroup from running concurrently with Close and
	// provides a deterministic semantics: either the OpenGroup completes
	// entirely before Close proceeds, or Close waits for OpenGroup to finish.
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	// Take a snapshot of active groups under lock. activeGroups == nil
	// indicates the track has been closed and prevents new groups.
	s.groupMapMu.Lock()
	if s.activeGroups == nil {
		s.groupMapMu.Unlock()
		return nil
	}
	groups := make([]*GroupWriter, 0, len(s.activeGroups))
	for g := range s.activeGroups {
		groups = append(groups, g)
	}
	// Prevent further additions and drop map
	s.activeGroups = nil
	s.groupMapMu.Unlock()

	for _, g := range groups {
		_ = g.Close()
	}

	// Then close the subscribe stream if present
	var err error
	if s.receiveSubscribeStream != nil {
		err = s.receiveSubscribeStream.close()
		s.receiveSubscribeStream = nil
	}

	if s.onCloseTrackFunc != nil {
		s.onCloseTrackFunc()
		s.onCloseTrackFunc = nil
	}

	return err
}

// CloseWithError stops publishing due to an error and cancels active groups.
func (s *TrackWriter) CloseWithError(code SubscribeErrorCode) {
	// Ensure CloseWithError is exclusive with OpenGroup.
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	s.groupMapMu.Lock()
	if s.activeGroups != nil {

		// Cancel all active groups first
		for group := range s.activeGroups {
			group.CancelWrite(PublishAbortedErrorCode)
		}
		s.activeGroups = nil

	}
	s.groupMapMu.Unlock()
	if s.receiveSubscribeStream != nil {
		_ = s.receiveSubscribeStream.closeWithError(code)
		s.receiveSubscribeStream = nil
	}

	if s.onCloseTrackFunc != nil {
		s.onCloseTrackFunc()
		s.onCloseTrackFunc = nil
	}
}

// OpenGroup opens a new group with an automatically incremented sequence number
// and returns a GroupWriter to write frames into it.
// The sequence starts at 1 and increments by 1 for each call.
func (s *TrackWriter) OpenGroup() (*GroupWriter, error) {
	// Atomically increment and get the next sequence
	seq := GroupSequence(s.groupSequence.Add(1))
	return s.openGroupWithSequence(seq)
}

// OpenGroupAt opens a new group with the specified sequence number.
// It advances the internal next-sequence counter to at least seq+1 so that
// subsequent OpenGroup calls will not produce a duplicate sequence.
func (s *TrackWriter) OpenGroupAt(seq GroupSequence) (*GroupWriter, error) {
	// Advance the internal counter to avoid collisions with subsequent
	// OpenGroup calls. CAS loop ensures correctness under concurrency.
	for {
		cur := s.groupSequence.Load()
		next := max(cur, uint64(seq)+1)
		if next == cur {
			break
		}
		if s.groupSequence.CompareAndSwap(cur, next) {
			break
		}
	}
	return s.openGroupWithSequence(seq)
}

// SkipGroups skips the next n group sequences without opening them.
// This is useful when you need to intentionally create gaps in the sequence,
// for example, when dropping groups due to packet loss or priority decisions.
func (s *TrackWriter) SkipGroups(n uint64) {
	s.groupSequence.Add(n)
}

func (s *TrackWriter) Context() context.Context {
	return s.receiveSubscribeStream.Context()
}

func (s *TrackWriter) WriteInfo(info Info) error {
	return s.receiveSubscribeStream.writeInfo(info)
}

func (s *TrackWriter) TrackConfig() *TrackConfig {
	if s.receiveSubscribeStream == nil {
		return &TrackConfig{}
	}
	return s.receiveSubscribeStream.TrackConfig()
}

func (s *TrackWriter) Updated() <-chan struct{} {
	if s.receiveSubscribeStream == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.receiveSubscribeStream.Updated()
}

// openGroupWithSequence is the internal implementation for opening a group with a specific sequence.
func (s *TrackWriter) openGroupWithSequence(seq GroupSequence) (*GroupWriter, error) {
	// Avoid accessing s.ctx directly; it can be nil if the receiveSubscribeStream
	// has been cleared during Close(). Instead, capture the receiveSubscribeStream
	// under lock and validate its context below.

	// Prevent opening a new group if the track has been closed. Capture
	// receiveSubscribeStream under lock so it cannot be set to nil by Close()
	// Acquire a shared lock so multiple OpenGroup calls can proceed
	// concurrently while ensuring Close waits for them to finish.
	s.closeMu.RLock()
	defer s.closeMu.RUnlock()

	// Check the context on the captured receiveSubscribeStream instead of s.ctx
	// to avoid nil deref if the embedded field has been cleared by Close().
	if s.Context().Err() != nil {
		return nil, Cause(s.Context())
	}

	// Write the INFO message to the receive subscribe stream.
	err := s.WriteInfo(Info{})
	if err != nil {
		return nil, err
	}

	stream, err := s.openUniStreamFunc()
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
		SubscribeID:   uint64(s.receiveSubscribeStream.subscribeID),
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

	var group *GroupWriter
	group = newGroupWriter(stream, seq, func() { s.removeGroup(group) })
	s.addGroup(group)

	return group, nil
}

func (s *TrackWriter) addGroup(group *GroupWriter) {
	s.groupMapMu.Lock()
	defer s.groupMapMu.Unlock()

	s.activeGroups[group] = struct{}{}
}

func (s *TrackWriter) removeGroup(group *GroupWriter) {
	s.groupMapMu.Lock()
	defer s.groupMapMu.Unlock()

	delete(s.activeGroups, group)
}
