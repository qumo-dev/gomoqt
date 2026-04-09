package moqt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/quic-go/quic-go"
)

// Session represents an active MOQ session over a QUIC connection.
// It manages bidirectional and unidirectional streams, subscriptions, and announcements for a single peer connection.
type Session struct {
	ctx context.Context // Context for the session

	wg sync.WaitGroup // WaitGroup for session cleanup

	conn StreamConn

	transport string // "quic" or "webtransport"

	mux *TrackMux

	subscribeIDCounter atomic.Uint64

	trackReaders         map[SubscribeID]*TrackReader
	trackReaderMapLocker sync.RWMutex

	trackWriters         map[SubscribeID]*TrackWriter
	trackWriterMapLocker sync.RWMutex

	fetchHandler FetchHandler

	isTerminating atomic.Bool
	sessErr       error

	sessionManager *sessionManager
}

func newSession(
	conn StreamConn,
	mux *TrackMux,
	manager *sessionManager,
	fetchHandler FetchHandler,
) *Session {
	if mux == nil {
		mux = DefaultMux
	}

	connCtx := conn.Context()
	sess := &Session{
		ctx:            connCtx,
		conn:           conn,
		mux:            mux,
		fetchHandler:   fetchHandler,
		trackReaders:   make(map[SubscribeID]*TrackReader),
		trackWriters:   make(map[SubscribeID]*TrackWriter),
		sessionManager: manager,
	}

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

	err := s.conn.CloseWithError(ConnErrorCode(code), msg)
	if err != nil {
		var appErr *ApplicationError
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

	if s.sessionManager != nil {
		sessionManager := s.sessionManager
		s.sessionManager = nil

		sessionManager.removeSession(s)
	}

	return nil
}

// Subscribe sends SUBSCRIBE and waits for SUBSCRIBE_OK.
// ctx is used while opening the stream, sending SUBSCRIBE, and waiting for the response.
func (s *Session) Subscribe(ctx context.Context, req *SubscribeRequest) (*TrackReader, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}

	if req == nil {
		return nil, errors.New("subscribe request cannot be nil")
	}
	req = req.Clone().normalized()

	if !isValidPath(req.BroadcastPath) {
		return nil, fmt.Errorf("invalid broadcast path: %q", req.BroadcastPath)
	}

	if s.terminating() {
		if s.sessErr == nil {
			return nil, ErrClosedSession
		}
		return nil, s.sessErr
	}

	id := s.nextSubscribeID()

	stream, err := s.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open bidirectional stream: %w", err)
	}

	err = message.StreamTypeSubscribe.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}
		cancelStreamWithError(stream, StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.SubscribeMessage{
		SubscribeID:          uint64(id),
		BroadcastPath:        string(req.BroadcastPath),
		TrackName:            string(req.TrackName),
		SubscriberPriority:   uint8(req.Config.Priority),
		SubscriberOrdered:    boolToWireFlag(req.Config.Ordered),
		SubscriberMaxLatency: req.Config.MaxLatency,
		StartGroup:           groupSequenceToWire(req.Config.StartGroup),
		EndGroup:             groupSequenceToWire(req.Config.EndGroup),
	}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}

		cancelStreamWithError(stream, StreamErrorCode(SubscribeErrorCodeInternal))

		return nil, fmt.Errorf("failed to encode SUBSCRIBE message: %w", err)
	}

	// Create and register a TrackReader for this subscription.
	substr := newSendSubscribeStream(id, stream, req.Config, PublishInfo{}, req.OnDrop)

	removeTrackFunc := func() {
		s.removeTrackReader(id)
	}
	track := newTrackReader(req, substr, removeTrackFunc)
	s.addTrackReader(id, track)

	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	select {
	case <-substr.wroteInfoChan:
	case <-ctx.Done():
		track.CloseWithError(SubscribeErrorCodeTimeout)
		return nil, fmt.Errorf("subscription timed out: %w", ctx.Err())
	}

	return track, nil
}

func readSubscribeResponse(stream io.Reader) (*message.SubscribeOkMessage, *message.SubscribeDropMessage, error) {
	head := make([]byte, 1)
	if _, err := io.ReadFull(stream, head); err != nil {
		return nil, nil, err
	}

	msgType, _, err := message.ReadVarint(head)
	if err != nil {
		return nil, nil, err
	}

	switch msgType {
	case 0x0:
		var msg message.SubscribeOkMessage
		err := msg.Decode(stream)
		if err != nil {
			return nil, nil, err
		}
		return &msg, nil, nil
	case 0x1:
		var msg message.SubscribeDropMessage
		err := msg.Decode(stream)
		if err != nil {
			return nil, nil, err
		}
		return nil, &msg, nil
	default:
		return nil, nil, fmt.Errorf("unexpected SUBSCRIBE response type: %d", msgType)
	}
}

// nextSubscribeID atomically increments and returns the next SubscribeID for new subscriptions.
func (s *Session) nextSubscribeID() SubscribeID {
	// Increment and return the previous value atomically
	return SubscribeID(s.subscribeIDCounter.Add(1))
}

func (s *Session) timeout() time.Duration {
	return 30 * time.Second
}

func (s *Session) Fetch(req *FetchRequest) (*GroupReader, error) {
	if s.terminating() {
		if s.sessErr == nil {
			return nil, ErrClosedSession
		}
		return nil, s.sessErr
	}

	stream, err := s.conn.OpenStream()
	if err != nil {
		var appErr *ApplicationError
		if errors.As(err, &appErr) {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open stream for fetch: %w", err)
	}

	err = message.StreamTypeFetch.Encode(stream)
	if err != nil {
		var strErr *StreamError
		if errors.As(err, &strErr) && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &FetchError{
				StreamError: strErr,
			}
		}
		strErrCode := StreamErrorCode(FetchErrorCodeInternal)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.FetchMessage{
		BroadcastPath: string(req.BroadcastPath),
		TrackName:     string(req.TrackName),
		Priority:      uint8(req.Priority),
		GroupSequence: uint64(req.GroupSequence),
	}.Encode(stream)
	if err != nil {
		var strErr *StreamError
		if errors.As(err, &strErr) && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &FetchError{
				StreamError: strErr,
			}
		}

		strErrCode := StreamErrorCode(FetchErrorCodeInternal)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)

		return nil, fmt.Errorf("failed to encode FETCH message: %w", err)
	}

	group := newGroupReader(req.GroupSequence, stream, nil)

	return group, nil
}

// AcceptAnnounce requests announcements from the remote peer that match the
// specified prefix. It opens an announce stream and returns an
// AnnouncementReader that yields Announcement objects for active tracks.
func (sess *Session) AcceptAnnounce(prefix string) (*AnnouncementReader, error) {
	if sess.terminating() {
		if sess.sessErr == nil {
			return nil, ErrClosedSession
		}
		return nil, sess.sessErr
	}

	stream, err := sess.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}

		return nil, fmt.Errorf("failed to open stream for announce: %w", err)
	}

	err = message.StreamTypeAnnounce.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*StreamError](err); ok {
			strErrCode := StreamErrorCode(InternalAnnounceErrorCode)
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
		if strErr, ok := errors.AsType[*StreamError](err); ok {
			strErrCode := StreamErrorCode(InternalAnnounceErrorCode)
			stream.CancelRead(strErrCode)

			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		strErrCode := StreamErrorCode(InternalAnnounceErrorCode)
		stream.CancelWrite(strErrCode)
		stream.CancelRead(strErrCode)

		return nil, fmt.Errorf("failed to send ANNOUNCE_PLEASE message: %w", err)
	}

	return newAnnouncementReader(stream, prefix, nil), nil
}

// Probe sends a bitrate probe request to the remote peer and returns the
// bitrate reported by the response.
func (sess *Session) Probe(bitrate uint64) (uint64, error) {
	if sess.terminating() {
		if sess.sessErr == nil {
			return 0, ErrClosedSession
		}
		return 0, sess.sessErr
	}

	stream, err := sess.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*ApplicationError](err); ok {
			return 0, &SessionError{
				ApplicationError: appErr,
			}
		}

		return 0, fmt.Errorf("failed to open stream for probe: %w", err)
	}
	defer stream.Close()

	err = message.StreamTypeProbe.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*StreamError](err); ok {
			stream.CancelRead(strErr.ErrorCode)
			return 0, err
		}

		cancelStreamWithError(stream, StreamErrorCode(ProbeErrorCodeInternal))

		return 0, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.ProbeMessage{Bitrate: bitrate}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*StreamError](err); ok {
			stream.CancelRead(strErr.ErrorCode)
			return 0, err
		}

		cancelStreamWithError(stream, StreamErrorCode(ProbeErrorCodeInternal))

		return 0, fmt.Errorf("failed to send PROBE message: %w", err)
	}

	var resp message.ProbeMessage
	err = resp.Decode(stream)
	if err != nil {
		return 0, fmt.Errorf("failed to read PROBE response: %w", err)
	}

	return resp.Bitrate, nil
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
// The function handles announce, subscribe, and info streams, and terminates the session
// if an unknown stream type is encountered.
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

func (sess *Session) processBiStream(stream Stream) {
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		_ = sess.CloseWithError(ProtocolViolationErrorCode, err.Error())
		return
	}
	defer stream.Close()

	switch streamType {
	case message.StreamTypeAnnounce:
		var apm message.AnnouncePleaseMessage
		err := apm.Decode(stream)
		if err != nil {
			cancelStreamWithError(stream, StreamErrorCode(InternalAnnounceErrorCode))
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
			cancelStreamWithError(stream, StreamErrorCode(SubscribeErrorCodeInternal))
			return
		}

		// Create a receiveSubscribeStream with draft3 fields decoded from SUBSCRIBE message
		config := &SubscribeConfig{
			Priority:   TrackPriority(sm.SubscriberPriority),
			Ordered:    boolFromWireFlag(sm.SubscriberOrdered),
			MaxLatency: sm.SubscriberMaxLatency,
		}

		// Decode 0-sentinel / +1-encoded fields (matching SUBSCRIBE_UPDATE logic)
		config.StartGroup = groupSequenceFromWire(sm.StartGroup)
		config.EndGroup = groupSequenceFromWire(sm.EndGroup)

		substr := newReceiveSubscribeStream(SubscribeID(sm.SubscribeID), stream, config)

		track := newTrackWriter(
			BroadcastPath(sm.BroadcastPath),
			TrackName(sm.TrackName),
			substr,
			sess.conn.OpenUniStream,
			func() { sess.removeTrackWriter(SubscribeID(sm.SubscribeID)) },
		)
		sess.addTrackWriter(SubscribeID(sm.SubscribeID), track)

		sess.mux.serveTrack(track)

		// Ensure the track writer is closed when done
		track.Close()
	case message.StreamTypeFetch:
		var fm message.FetchMessage
		err := fm.Decode(stream)
		if err != nil {
			cancelStreamWithError(stream, StreamErrorCode(FetchErrorCodeInternal))
			return
		}

		handler := sess.fetchHandler
		if handler == nil {
			cancelStreamWithError(stream, StreamErrorCode(FetchErrorCodeInternal))
			return
		}

		req := &FetchRequest{
			BroadcastPath: BroadcastPath(fm.BroadcastPath),
			TrackName:     TrackName(fm.TrackName),
			Priority:      TrackPriority(fm.Priority),
			GroupSequence: GroupSequence(fm.GroupSequence),
		}

		w := newGroupWriter(stream, req.GroupSequence, nil)

		handler.ServeFetch(w, req)
	case message.StreamTypeProbe:
		if err := sess.handleProbeStream(stream); err != nil {
			cancelStreamWithError(stream, StreamErrorCode(ProbeErrorCodeInternal))
			return
		}
	default:
		cancelStreamWithError(stream, StreamErrorCode(InternalSessionErrorCode))
		return
	}
}

func (sess *Session) handleUniStreams() {
	for {
		stream, err := sess.conn.AcceptUniStream(sess.ctx)
		if err != nil {
			return
		}

		go sess.processUniStream(stream)
	}
}

func (sess *Session) processUniStream(stream ReceiveStream) {
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		return
	}
	defer stream.CancelRead(StreamErrorCode(InternalSessionErrorCode))

	switch streamType {
	case message.StreamTypeGroup:
		var gm message.GroupMessage
		err := gm.Decode(stream)
		if err != nil {
			return
		}

		track, ok := sess.trackReaders[SubscribeID(gm.SubscribeID)]
		if !ok {
			stream.CancelRead(StreamErrorCode(InvalidSubscribeIDErrorCode))
			return
		}

		// Enqueue the receiver
		track.enqueueGroup(GroupSequence(gm.GroupSequence), stream)
	default:
		// Unknown stream types are stream-local and non-fatal for extension probing.
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

func cancelStreamWithError(stream Stream, code StreamErrorCode) {
	stream.CancelRead(code)
	stream.CancelWrite(code)
}

func (sess *Session) handleProbeStream(stream Stream) error {
	provider, ok := sess.probeStatsProvider()
	if !ok {
		return errors.New("probe unsupported")
	}

	tracker := &probeMeasurementTracker{}
	for {
		var pm message.ProbeMessage
		err := pm.Decode(stream)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		measured := tracker.measure(provider.ConnectionStats(), pm.Bitrate, time.Now())
		if err := (message.ProbeMessage{Bitrate: measured}).Encode(stream); err != nil {
			return err
		}
	}
}

type probeStatsProvider interface {
	ConnectionStats() quic.ConnectionStats
}

type probeMeasurementTracker struct {
	initialized bool
	bytesSent   uint64
	sampleTime  time.Time
}

func (sess *Session) probeStatsProvider() (probeStatsProvider, bool) {
	provider, ok := sess.conn.(probeStatsProvider)
	return provider, ok
}

func (t *probeMeasurementTracker) measure(stats quic.ConnectionStats, fallback uint64, now time.Time) uint64 {
	if !t.initialized {
		t.initialized = true
		t.bytesSent = stats.BytesSent
		t.sampleTime = now
		return fallback
	}

	elapsed := now.Sub(t.sampleTime)
	if elapsed <= 0 {
		return fallback
	}

	bytesSent := stats.BytesSent
	bytesDelta := bytesSent
	if bytesDelta >= t.bytesSent {
		bytesDelta -= t.bytesSent
	} else {
		bytesDelta = 0
	}
	if bytesDelta == 0 {
		t.bytesSent = bytesSent
		t.sampleTime = now
		return fallback
	}

	t.bytesSent = bytesSent
	t.sampleTime = now
	return uint64((float64(bytesDelta) * 8) / elapsed.Seconds())
}
