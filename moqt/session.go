package moqt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

func newSession(conn transport.StreamConn, args ...any) *Session {
	var mux *TrackMux
	var onClose func()

	switch len(args) {
	case 2:
		if m, ok := args[0].(*TrackMux); ok {
			mux = m
		}
		if f, ok := args[1].(func()); ok {
			onClose = f
		}
	case 3:
		// Backward compatibility for old test signatures:
		// newSession(conn, sessStream, mux, onClose)
		if m, ok := args[1].(*TrackMux); ok {
			mux = m
		}
		if f, ok := args[2].(func()); ok {
			onClose = f
		}
	default:
		panic("newSession: invalid arguments")
	}

	if mux == nil {
		mux = DefaultMux
	}

	connCtx := conn.Context()
	sess := &Session{
		ctx:          connCtx,
		conn:         conn,
		mux:          mux,
		trackReaders: make(map[SubscribeID]*TrackReader),
		trackWriters: make(map[SubscribeID]*TrackWriter),
		onClose:      onClose,
	}

	// Supervise the session stream closure
	context.AfterFunc(connCtx, func() {
		reason := connCtx.Err()
		var appErr *transport.ApplicationError
		if errors.As(reason, &appErr) {
			return // Normal closure
		}

		_ = sess.CloseWithError(ProtocolViolationErrorCode, "session stream closed unexpectedly")
	})

	// Listen bidirectional streams
	sess.wg.Go(func() {
		sess.handleBiStreams()
	})

	// Listen unidirectional streams
	sess.wg.Go(func() {
		sess.handleUniStreams()
	})

	return sess
}

// Session represents an active MOQ session over a QUIC connection.
// It manages bidirectional and unidirectional streams, subscriptions, and announcements for a single peer connection.
type Session struct {
	ctx context.Context // Context for the session

	wg sync.WaitGroup // WaitGroup for session cleanup

	conn transport.StreamConn

	transport string // "quic" or "webtransport"

	mux *TrackMux // TODO

	subscribeIDCounter atomic.Uint64

	trackReaders         map[SubscribeID]*TrackReader
	trackReaderMapLocker sync.RWMutex

	trackWriters         map[SubscribeID]*TrackWriter
	trackWriterMapLocker sync.RWMutex

	isTerminating atomic.Bool
	sessErr       error

	onClose func() // Function to call when the session is closed
}

func (s *Session) terminating() bool {
	return s.isTerminating.Load()
}

// Context returns the session's context which is canceled when the session
// terminates. Use it to observe session lifecycle and cancellation.
func (s *Session) Context() context.Context {
	return s.ctx
}

// LocalAddr returns the local network address.
// The returned value implements [TransportAddr], which can be used to
// retrieve the transport protocol ("quic" or "webtransport").
func (s *Session) LocalAddr() net.Addr {
	if s == nil || s.conn == nil {
		return nil
	}
	return &addr{addr: s.conn.LocalAddr(), transport: s.transport}
}

// RemoteAddr returns the remote network address of the peer.
// The returned value implements [TransportAddr], which can be used to
// retrieve the transport protocol ("quic" or "webtransport"):
//
//	if ta, ok := sess.RemoteAddr().(moqt.TransportAddr); ok {
//	    fmt.Println(ta.Transport()) // "quic" or "webtransport"
//	}
func (s *Session) RemoteAddr() net.Addr {
	if s == nil || s.conn == nil {
		return nil
	}
	return &addr{addr: s.conn.RemoteAddr(), transport: s.transport}
}

// CloseWithError closes the connection with an error code and message.
func (s *Session) CloseWithError(code SessionErrorCode, msg string) error {
	if s.terminating() {
		return s.sessErr
	}
	s.isTerminating.Store(true)

	if s.onClose != nil {
		s.onClose()
	}

	err := s.conn.CloseWithError(transport.ConnErrorCode(code), msg)
	if err != nil {
		var appErr *transport.ApplicationError
		if errors.As(err, &appErr) {
			reason := &SessionError{
				ApplicationError: appErr,
			}
			s.sessErr = reason
			return reason
		}
		s.sessErr = err
		return fmt.Errorf("session termination failed: %w", err)
	}

	// Wait for finishing handling streams
	s.wg.Wait()

	return nil
}

func (s *Session) Subscribe(path BroadcastPath, name TrackName, config *TrackConfig) (*TrackReader, error) {
	if s.terminating() {
		if s.sessErr == nil {
			return nil, ErrClosedSession
		}
		return nil, s.sessErr
	}

	if config == nil {
		config = &TrackConfig{}
	}

	id := s.nextSubscribeID()

	stream, err := s.conn.OpenStream()
	if err != nil {
		var appErr *transport.ApplicationError
		if errors.As(err, &appErr) {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open bidirectional stream: %w", err)
	}

	err = message.StreamTypeSubscribe.Encode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}
		strErrCode := transport.StreamErrorCode(InternalSubscribeErrorCode)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	// Send a SUBSCRIBE message
	sm := message.SubscribeMessage{
		SubscribeID:   uint64(id),
		BroadcastPath: string(path),
		TrackName:     string(name),
		TrackPriority: uint8(config.TrackPriority),
	}
	err = sm.Encode(stream)
	if err == nil {
		// Message sent successfully
	}
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}

		strErrCode := transport.StreamErrorCode(InternalSubscribeErrorCode)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)

		return nil, fmt.Errorf("failed to encode SUBSCRIBE message: %w", err)
	}

	// Register TrackReader AFTER sending SUBSCRIBE but BEFORE waiting for SUBSCRIBE_OK
	// This ensures we're ready to receive data streams immediately when server approves
	substr := newSendSubscribeStream(id, stream, config, Info{})

	// Create a receive group stream queue
	trackReceiver := newTrackReader(
		path,
		name,
		substr,
		func() { s.removeTrackReader(id) },
	)
	s.addTrackReader(id, trackReceiver)

	cleanup := func() {
		s.removeTrackReader(id)
	}

	var subok message.SubscribeOkMessage
	err = subok.Decode(stream)
	if err != nil {
		cleanup()
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			strErrCode := transport.StreamErrorCode(strErr.ErrorCode)
			stream.CancelWrite(strErrCode)

			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}

		// If Decode returned an error that's not a QUIC StreamError, fail.
		// For non-QUIC stream errors (e.g., io.EOF), do not cancel the stream
		// aggressively. Allow the caller to close the session or the remote to
		// clean up; canceling here may trigger a remote Reset which can break
		// the normal lifecycle and lead to spurious EOFs on the server side.
		return nil, fmt.Errorf("failed to read SUBSCRIBE_OK response: %w", err)
	} else {
		// Successful receipt of SUBSCRIBE_OK.
	}

	return trackReceiver, nil
}

// Subscribe starts a subscription for the specified broadcast path and track name within the session.
// It returns a TrackReader that can be used to accept groups and read track data.
// The returned TrackReader and the subscription exist for the lifetime of this session unless closed.

func (s *Session) nextSubscribeID() SubscribeID {
	// Increment and return the previous value atomically
	return SubscribeID(s.subscribeIDCounter.Add(1))
}

func (sess *Session) AcceptAnnounce(prefix string) (*AnnouncementReader, error) {
	if sess.terminating() {
		if sess.sessErr == nil {
			return nil, ErrClosedSession
		}
		return nil, sess.sessErr
	}

	stream, err := sess.conn.OpenStream()
	if err != nil {
		var appErr *transport.ApplicationError
		if errors.As(err, &appErr) {

			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}

		return nil, fmt.Errorf("failed to open stream for announce: %w", err)
	}

	err = message.StreamTypeAnnounce.Encode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			strErrCode := transport.StreamErrorCode(InternalAnnounceErrorCode)
			stream.CancelRead(strErrCode)

			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.AnnouncePleaseMessage{
		TrackPrefix: prefix,
	}.Encode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			strErrCode := transport.StreamErrorCode(InternalAnnounceErrorCode)
			stream.CancelRead(strErrCode)

			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		strErrCode := transport.StreamErrorCode(InternalAnnounceErrorCode)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)

		return nil, fmt.Errorf("failed to send ANNOUNCE_PLEASE message: %w", err)
	}

	var aim message.AnnounceInitMessage
	err = aim.Decode(stream)
	if err != nil {
		var strErr *transport.StreamError
		if errors.As(err, &strErr) {
			// Helpful debug logging for interop investigation: show exact stream error details
			strErrCode := transport.StreamErrorCode(InternalAnnounceErrorCode)
			stream.CancelRead(strErrCode)

			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		return nil, fmt.Errorf("failed to read ANNOUNCE_INIT message: %w", err)
	}

	return newAnnouncementReader(stream, prefix, aim.Suffixes), nil
}

// AcceptAnnounce requests announcements from the remote peer that match the
// specified prefix. It opens an announce stream and returns an
// AnnouncementReader that yields Announcement objects for active tracks.

func (sess *Session) goAway(_ string) error {
	// goAway is a no-op in MOQT. Graceful shutdown is handled by the
	// underlying QUIC connection close. This method exists for API
	// compatibility with server/client shutdown logic.
	return nil
}

// listenBiStreams accepts bidirectional streams and handles them based on their type.
// It listens for incoming streams and processes them in separate goroutines.
// The function handles session streams, announce streams, subscribe streams, and info streams.
// It also handles errors and terminates the session if an unknown stream type is encountered.
func (sess *Session) handleBiStreams() {
	for { // Accept a bidirectional stream
		stream, err := sess.conn.AcceptStream(sess.ctx)
		if err != nil {
			return
		}

		// Handle the stream
		go sess.processBiStream(stream)
	}
}

func (sess *Session) processBiStream(stream transport.Stream) {
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		_ = sess.CloseWithError(ProtocolViolationErrorCode, err.Error())
		return
	}

	switch streamType {
	case message.StreamTypeAnnounce:
		var apm message.AnnouncePleaseMessage
		err := apm.Decode(stream)
		if err != nil {
			cancelStreamWithError(stream, transport.StreamErrorCode(InternalAnnounceErrorCode))
			return
		}

		prefix := apm.TrackPrefix

		annstr := newAnnouncementWriter(stream, prefix)

		sess.mux.serveAnnouncements(annstr)

		// Ensure the announcement writer is closed when done
		annstr.Close()
	case message.StreamTypeSubscribe:
		var sm message.SubscribeMessage
		err := sm.Decode(stream)
		if err != nil {
			cancelStreamWithError(stream, transport.StreamErrorCode(InternalSubscribeErrorCode))
			return
		}

		// Create a receiveSubscribeStream
		config := &TrackConfig{
			TrackPriority: TrackPriority(sm.TrackPriority),
		}

		substr := newReceiveSubscribeStream(SubscribeID(sm.SubscribeID), stream, config)

		w := newTrackWriter(
			BroadcastPath(sm.BroadcastPath),
			TrackName(sm.TrackName),
			substr,
			sess.conn.OpenUniStream,
			func() { sess.removeTrackWriter(SubscribeID(sm.SubscribeID)) },
		)
		sess.addTrackWriter(SubscribeID(sm.SubscribeID), w)

		sess.mux.serveTrack(w)

		// Ensure the track writer is closed when done
		w.Close()
	default:
		_ = sess.CloseWithError(ProtocolViolationErrorCode, fmt.Sprintf("unknown bidirectional stream type: %v", streamType))
		return
	}
}

func (sess *Session) handleUniStreams() {
	for {
		/*
		 * Accept a unidirectional stream
		 */
		stream, err := sess.conn.AcceptUniStream(sess.ctx)
		if err != nil {
			return
		}

		// Handle the stream
		go sess.processUniStream(stream)
	}
}

func (sess *Session) processUniStream(stream transport.ReceiveStream) {
	/*
	 * Get a Stream Type ID
	 */
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		return
	}

	// Handle the stream by the Stream Type ID
	switch streamType {
	case message.StreamTypeGroup:
		var gm message.GroupMessage
		err := gm.Decode(stream)
		if err != nil {
			return
		}

		track, ok := sess.trackReaders[SubscribeID(gm.SubscribeID)]
		if !ok {
			stream.CancelRead(transport.StreamErrorCode(InvalidSubscribeIDErrorCode))
			return
		}

		// Enqueue the receiver
		track.enqueueGroup(GroupSequence(gm.GroupSequence), stream)
	default:
		// Terminate the session
		_ = sess.CloseWithError(ProtocolViolationErrorCode, fmt.Sprintf("unknown unidirectional stream type: %v", streamType))
		return
	}
}

func (s *Session) addTrackWriter(id SubscribeID, writer *TrackWriter) {
	s.trackWriterMapLocker.Lock()
	defer s.trackWriterMapLocker.Unlock()

	s.trackWriters[id] = writer
}

func (s *Session) removeTrackWriter(id SubscribeID) {
	s.trackWriterMapLocker.Lock()
	defer s.trackWriterMapLocker.Unlock()

	delete(s.trackWriters, id)
}

func (s *Session) addTrackReader(id SubscribeID, reader *TrackReader) {
	s.trackReaderMapLocker.Lock()
	defer s.trackReaderMapLocker.Unlock()

	s.trackReaders[id] = reader
}

func (s *Session) removeTrackReader(id SubscribeID) {
	s.trackReaderMapLocker.Lock()
	defer s.trackReaderMapLocker.Unlock()

	delete(s.trackReaders, id)
}

func cancelStreamWithError(stream transport.Stream, code transport.StreamErrorCode) {
	stream.CancelRead(code)
	stream.CancelWrite(code)
}
