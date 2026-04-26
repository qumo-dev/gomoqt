package moqt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/moqttrace"
	"github.com/qumo-dev/gomoqt/transport"
)

const (
	moqtVersion = "moq-lite-04"
)

// Session represents an active MOQ session over a QUIC connection.
// It manages bidirectional and unidirectional streams, subscriptions, and
// announcements for a single peer connection.
type Session struct {
	ctx    context.Context // Context for the session
	config *Config

	wg sync.WaitGroup // WaitGroup for session cleanup

	conn StreamConn

	mux *TrackMux

	subscribeIDCounter atomic.Uint64

	trackReaders         map[SubscribeID]*TrackReader
	trackReaderMapLocker sync.RWMutex

	trackWriters         map[SubscribeID]*TrackWriter
	trackWriterMapLocker sync.RWMutex

	fetchHandler FetchHandler
	onGoaway     func(newSessionURI string)
	logger       *slog.Logger

	isTerminating atomic.Bool
	// sessErr       error

	connManager *connManager

	// probe stream state (subscriber side, lazily initialized)
	outgoingProbeMu     sync.Mutex
	outgoingProbeStream transport.Stream
	probeResponseCh     chan ProbeResult

	// incoming probe stream state (publisher side)
	incomingProbeMu     sync.Mutex
	incomingProbeStream transport.Stream
	probeTargetsCh      chan ProbeResult

	bitrateTracker bitrateTracker

	trace *moqttrace.SessionTrace
}

func newSession(
	conn StreamConn,
	mux *TrackMux,
	manager *connManager,
	config *Config,
	fetchHandler FetchHandler,
	onGoaway func(newSessionURI string),
	logger *slog.Logger,
) *Session {
	if mux == nil {
		mux = DefaultMux
	}

	connCtx := conn.Context()
	sess := &Session{
		ctx:             connCtx,
		config:          config.Clone(),
		conn:            conn,
		mux:             mux,
		fetchHandler:    fetchHandler,
		onGoaway:        onGoaway,
		logger:          logger,
		trackReaders:    make(map[SubscribeID]*TrackReader),
		trackWriters:    make(map[SubscribeID]*TrackWriter),
		connManager:     manager,
		probeResponseCh: make(chan ProbeResult, 1), // latest-value semantics
		probeTargetsCh:  make(chan ProbeResult, 1), // latest-value semantics
		bitrateTracker: bitrateTracker{
			maxAge:   config.probeMaxAge(),
			maxDelta: config.probeMaxDelta(),
		},
		trace: moqttrace.SessionTraceFromContext(connCtx),
	}

	if manager != nil {
		manager.addConn(conn)
	}

	if provider, ok := conn.(probeStatsProvider); ok {
		sess.wg.Go(func() {
			sess.detectBitrateChanges(provider)
		})
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

func (s *Session) logError(msg string, err error, args ...any) {
	if s == nil || err == nil {
		return
	}

	if s.logger != nil {
		s.logger.Error(msg, append(args, "error", err)...)
	}
}

// Context returns the session's context which is canceled when the session
// terminates. Use it to observe session lifecycle and cancellation.
func (s *Session) Context() context.Context {
	return s.ctx
}

// ConnectionState returns connection metadata for the session.
func (s *Session) ConnectionState() ConnectionState {
	return ConnectionState{
		Version: moqtVersion,
		TLS:     s.conn.TLS(),
	}
}

// LocalAddr returns the local network address.
func (s *Session) LocalAddr() net.Addr {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr()
}

// RemoteAddr returns the remote network address of the peer.
func (s *Session) RemoteAddr() net.Addr {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.RemoteAddr()
}

// Stats returns a point-in-time snapshot of the session's operational metrics.
// It never returns an error; fields that cannot be measured on the current
// transport (e.g. RTT on a WebTransport/Browser session) are zero.
func (s *Session) Stats() SessionStats {
	var stats SessionStats
	stats.EstimatedBitrate = s.bitrateTracker.getEstimatedBitrate()

	if provider, ok := s.conn.(probeStatsProvider); ok {
		cs := provider.ConnectionStats()
		stats.RTT = cs.SmoothedRTT
		stats.BytesSent = cs.BytesSent
		stats.BytesReceived = cs.BytesReceived
	}

	return stats
}

// CloseWithError closes the session with an error code and message.
func (s *Session) CloseWithError(code SessionErrorCode, msg string) error {
	if s.terminating() {
		return nil
	}
	s.isTerminating.Store(true)

	err := s.conn.CloseWithError(transport.ConnErrorCode(code), msg)
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			reason := &SessionError{
				ApplicationError: appErr,
			}
			return reason
		}
		return fmt.Errorf("session termination failed: %w", err)
	}

	// Wait for finishing handling streams
	s.wg.Wait()

	close(s.probeResponseCh)
	close(s.probeTargetsCh)

	if s.connManager != nil {
		connManager := s.connManager
		s.connManager = nil

		connManager.removeConn(s.conn)
	}

	return nil
}

// Subscribe sends SUBSCRIBE and waits for SUBSCRIBE_OK.
// ctx is used while opening the stream, sending SUBSCRIBE, and waiting for the response.
// If config is nil, a zero-value SubscribeConfig is used.
func (s *Session) Subscribe(ctx context.Context, path BroadcastPath, name TrackName, config *SubscribeConfig) (*TrackReader, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}

	if s.terminating() {
		return nil, ErrClosedSession
	}

	if !isValidPath(path) {
		return nil, fmt.Errorf("invalid broadcast path: %q", path)
	}

	if config == nil {
		config = &SubscribeConfig{}
	}

	id := s.nextSubscribeID()

	stream, err := s.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open bidirectional stream: %w", err)
	}

	err = message.StreamTypeSubscribe.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.SubscribeMessage{
		SubscribeID:          uint64(id),
		BroadcastPath:        string(path),
		TrackName:            string(name),
		SubscriberPriority:   uint8(config.Priority),
		SubscriberOrdered:    boolToWireFlag(config.Ordered),
		SubscriberMaxLatency: config.MaxLatency,
		StartGroup:           groupSequenceToWire(config.StartGroup),
		EndGroup:             groupSequenceToWire(config.EndGroup),
	}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &SubscribeError{
				StreamError: strErr,
			}
		}

		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))

		return nil, fmt.Errorf("failed to encode SUBSCRIBE message: %w", err)
	}

	substr := newSendSubscribeStream(id, stream, config, s.trace)

	track := newTrackReader(path, name, substr, func() { s.removeTrackReader(id) })
	s.addTrackReader(id, track)
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetReadDeadline(deadline)
		defer stream.SetReadDeadline(time.Time{})
	}

	okMsg, dropMsg, err := readSubscribeResponse(stream)
	if err != nil {
		if ctx.Err() != nil {
			cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeTimeout))
			return nil, fmt.Errorf("subscription timed out: %w", ctx.Err())
		}
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return nil, &SubscribeError{StreamError: strErr}
		}
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to read SUBSCRIBE response: %w", err)
	}

	if dropMsg != nil {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("moqt: unexpected SUBSCRIBE_DROP message received")
	}

	if okMsg == nil {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("moqt: expected SUBSCRIBE_OK message but got none")
	}

	substr.updateInfo(PublishInfo{
		Priority:   TrackPriority(okMsg.PublisherPriority),
		Ordered:    boolFromWireFlag(okMsg.PublisherOrdered),
		MaxLatency: okMsg.PublisherMaxLatency,
		StartGroup: groupSequenceFromWire(okMsg.StartGroup),
		EndGroup:   groupSequenceFromWire(okMsg.EndGroup),
	})
	go substr.readSubscribeResponses()

	return track, nil
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
		return nil, ErrClosedSession
	}

	stream, err := s.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open stream for fetch: %w", err)
	}

	err = message.StreamTypeFetch.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &FetchError{
				StreamError: strErr,
			}
		}
		cancelStreamWithError(stream, transport.StreamErrorCode(FetchErrorCodeInternal))
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.FetchMessage{
		BroadcastPath: string(req.BroadcastPath),
		TrackName:     string(req.TrackName),
		Priority:      uint8(req.Priority),
		GroupSequence: uint64(req.GroupSequence),
	}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok && strErr.Remote {
			stream.CancelRead(strErr.ErrorCode)
			return nil, &FetchError{
				StreamError: strErr,
			}
		}

		cancelStreamWithError(stream, transport.StreamErrorCode(FetchErrorCodeInternal))

		return nil, fmt.Errorf("failed to encode FETCH message: %w", err)
	}

	group := newGroupReader(req.GroupSequence, stream, nil)

	context.AfterFunc(req.Context(), func() {
		// Cancel the stream when the context is done
		group.CancelRead(ExpiredGroupErrorCode)
	})

	return group, nil
}

// AcceptAnnounce requests announcements from the remote peer that match the
// specified prefix. It opens an announce stream and returns an
// AnnouncementReader that yields Announcement objects for active tracks.
func (sess *Session) AcceptAnnounce(prefix string) (*AnnouncementReader, error) {
	if sess.terminating() {
		return nil, ErrClosedSession
	}

	stream, err := sess.conn.OpenStream()
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}

		return nil, fmt.Errorf("failed to open stream for announce: %w", err)
	}

	err = message.StreamTypeAnnounce.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			strErrCode := transport.StreamErrorCode(AnnounceErrorCodeInternal)
			stream.CancelRead(strErrCode)

			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.AnnounceInterestMessage{
		BroadcastPathPrefix: prefix,
		ExcludeHop:          sess.mux.hopID,
	}.Encode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			cancelStreamWithError(stream, transport.StreamErrorCode(AnnounceErrorCodeInternal))
			return nil, &AnnounceError{
				StreamError: strErr,
			}
		}

		cancelStreamWithError(stream, transport.StreamErrorCode(AnnounceErrorCodeInternal))

		return nil, fmt.Errorf("failed to send ANNOUNCE_INTEREST message: %w", err)
	}

	return newAnnouncementReader(stream, prefix, nil), nil
}

// SessionStats is a point-in-time snapshot of a Session's operational metrics.
// It is safe to copy by value and never returns an error.
//
// The design follows the NATS [nats.Statistics] pattern: a single flat struct
// containing all observable values, with zero as the canonical "not available"
// sentinel (e.g. RTT and byte counters are zero on WebTransport/Browser sessions
// where the underlying transport does not expose them).
type SessionStats struct {
	// EstimatedBitrate is the most recently measured outbound bitrate in bits
	// per second, derived from the Probe mechanism. Zero until the first
	// measurement is available.
	EstimatedBitrate uint64

	// RTT is the smoothed round-trip time as reported by the QUIC congestion
	// controller (RFC 9002 §5.3). Zero when the underlying transport does not
	// expose RTT (e.g. WebTransport browser sessions).
	RTT time.Duration
	// BytesSent is the cumulative number of bytes sent on the underlying
	// connection, excluding UDP framing. Zero when unavailable.
	BytesSent uint64
	// BytesReceived is the cumulative number of bytes received on the
	// underlying connection, excluding UDP framing. Zero when unavailable.
	BytesReceived uint64
}

// ProbeResult holds the result of a Probe request.
type ProbeResult struct {
	// Bitrate is the measured bitrate in bits per second. A value of 0 means unknown.
	Bitrate uint64
}

// Probe sends a target bitrate hint to the publisher and returns a channel
// that receives the measured bitrate reported by the publisher.
// Calling Probe again on the same session updates the target bitrate.
// The channel is closed when the probe stream ends or the session terminates.
func (sess *Session) Probe(targetBitrate uint64) (<-chan ProbeResult, error) {
	if sess.terminating() {
		return nil, ErrClosedSession
	}

	sess.outgoingProbeMu.Lock()
	defer sess.outgoingProbeMu.Unlock()

	probeStream := sess.outgoingProbeStream
	// Lazily open the probe stream.
	if probeStream == nil || probeStream.Context().Err() != nil {
		stream, err := sess.conn.OpenStream()
		if err != nil {
			if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
				return nil, &SessionError{ApplicationError: appErr}
			}
			return nil, fmt.Errorf("failed to open stream for probe: %w", err)
		}

		if err := message.StreamTypeProbe.Encode(stream); err != nil {
			if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
				stream.CancelRead(strErr.ErrorCode)
				return nil, err
			}
			cancelStreamWithError(stream, transport.StreamErrorCode(ProbeErrorCodeInternal))
			return nil, fmt.Errorf("failed to encode stream type message: %w", err)
		}

		sess.wg.Go(func() {
			// Read PROBE responses until the stream is closed or an error occurs.
			streamCtx := stream.Context()
			for {
				var pm message.ProbeMessage
				if err := pm.Decode(stream); err != nil {
					if !errors.Is(err, io.EOF) {
						sess.logError("failed to decode PROBE message", err)
						cancelStreamWithError(stream, transport.StreamErrorCode(ProbeErrorCodeInternal))
					}
					return
				}
				sess.bitrateTracker.record(pm.Bitrate, time.Now())

				// Update the latest probe result, dropping it if the channel buffer is full (i.e. the previous value has not been consumed).
				select {
				case <-sess.probeResponseCh:
				default:
				}
				select {
				case sess.probeResponseCh <- ProbeResult{Bitrate: pm.Bitrate}:
				default:
				}

				select {
				case <-streamCtx.Done():
					return
				default:
				}
			}
		})

		probeStream = stream
	}

	// Send PROBE with the new target bitrate. Per draft4 the subscriber MAY send
	// additional PROBE messages on the same stream to update the target.
	err := message.ProbeMessage{
		Bitrate: targetBitrate,
		RTT:     0,
	}.Encode(probeStream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			probeStream.CancelRead(strErr.ErrorCode)
			return nil, err
		}
		cancelStreamWithError(probeStream, transport.StreamErrorCode(ProbeErrorCodeInternal))

		return nil, fmt.Errorf("failed to send probe message: %w", err)
	}

	sess.outgoingProbeStream = probeStream

	return sess.probeResponseCh, nil
}

// ProbeTargets returns a channel that receives the latest target bitrate (bits
// per second) sent by the subscriber via PROBE messages. The channel has a
// buffer of 1 and uses latest-value semantics: if the previous value has not
// been consumed, it is replaced by the newer one.
//
// This is the publisher-side counterpart of [Session.Probe].
func (sess *Session) ProbeTargets() <-chan ProbeResult {
	return sess.probeTargetsCh
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

func (sess *Session) processBiStream(stream transport.Stream) {
	defer stream.Close()
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		sess.logError("failed to decode stream type", err)
		return
	}

	if t := sess.trace; t != nil && t.IncomingBiStreamStart != nil {
		t.IncomingBiStreamStart(uint8(streamType))
	}

	switch streamType {
	case message.StreamTypeAnnounce:
		var aim message.AnnounceInterestMessage
		err := aim.Decode(stream)
		if err != nil {
			sess.logError("failed to decode ANNOUNCE_INTEREST message", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(AnnounceErrorCodeInternal))
			return
		}

		prefix := aim.BroadcastPathPrefix

		annstr := newAnnouncementWriter(stream, prefix, sess.mux.hopID, aim.ExcludeHop, sess.logger)

		sess.mux.serveAnnouncements(annstr)

		// Ensure the announcement writer is closed when done
		annstr.Close()
	case message.StreamTypeSubscribe:
		var sm message.SubscribeMessage
		err := sm.Decode(stream)
		if err != nil {
			sess.logError("failed to decode SUBSCRIBE message", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
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
			sess.logError("failed to decode FETCH message", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(FetchErrorCodeInternal))
			return
		}

		handler := sess.fetchHandler

		req := &FetchRequest{
			BroadcastPath: BroadcastPath(fm.BroadcastPath),
			TrackName:     TrackName(fm.TrackName),
			Priority:      TrackPriority(fm.Priority),
			GroupSequence: GroupSequence(fm.GroupSequence),
			ctx:           stream.Context(),
		}

		group := newGroupWriter(stream, req.GroupSequence, nil)

		stop := context.AfterFunc(req.Context(), func() {
			// Cancel the stream when the context is done
			group.CancelWrite(ExpiredGroupErrorCode)
		})
		defer stop()

		err = safeServeFetch(handler, group, req)
		if err != nil {
			sess.logError("fetch handler error", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(FetchErrorCodeInternal))
			return
		}
	case message.StreamTypeProbe:
		err := sess.handleProbeStream(stream)
		if err != nil {
			sess.logError("probe stream error", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(ProbeErrorCodeInternal))
			return
		}
	case message.StreamTypeGoaway:
		if err := sess.handleGoawayStream(stream); err != nil {
			sess.logError("goaway stream error", err)
			cancelStreamWithError(stream, transport.StreamErrorCode(InternalSessionErrorCode))
			return
		}
	default:
		sess.logError("unknown stream type", fmt.Errorf("stream type %d", streamType))
		cancelStreamWithError(stream, transport.StreamErrorCode(InternalSessionErrorCode))
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

func (sess *Session) processUniStream(stream transport.ReceiveStream) {
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		sess.logError("failed to decode uni stream type", err)
		return
	}

	if t := sess.trace; t != nil && t.IncomingUniStreamStart != nil {
		t.IncomingUniStreamStart(uint8(streamType))
	}

	switch streamType {
	case message.StreamTypeGroup:
		var gm message.GroupMessage
		err := gm.Decode(stream)
		if err != nil {
			sess.logError("failed to decode GROUP message", err)
			return
		}

		track, ok := sess.trackReaders[SubscribeID(gm.SubscribeID)]
		if !ok {
			stream.CancelRead(transport.StreamErrorCode(InvalidSubscribeIDErrorCode))
			return
		}

		// Enqueue the receiver — ownership of the stream transfers to the TrackReader.
		track.enqueueGroup(GroupSequence(gm.GroupSequence), stream)
	default:
		// Unknown stream types are stream-local and non-fatal for extension probing.
		sess.logError("unknown uni stream type", fmt.Errorf("stream type %d", streamType))
		stream.CancelRead(transport.StreamErrorCode(InternalSessionErrorCode))
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

func (sess *Session) handleProbeStream(stream transport.Stream) error {
	sess.incomingProbeMu.Lock()
	if sess.incomingProbeStream != nil {
		cancelStreamWithError(sess.incomingProbeStream, transport.StreamErrorCode(ProbeErrorCodeInternal))
	}
	sess.incomingProbeStream = stream
	sess.incomingProbeMu.Unlock()

	defer func() {
		sess.incomingProbeMu.Lock()
		if sess.incomingProbeStream == stream {
			sess.incomingProbeStream = nil
		}
		sess.incomingProbeMu.Unlock()
	}()

	for {
		var pm message.ProbeMessage
		if err := pm.Decode(stream); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		// Update the latest probe target, dropping it if the channel buffer is full (i.e. the previous value has not been consumed).
		select {
		case <-sess.probeTargetsCh:
		default:
		}
		select {
		case sess.probeTargetsCh <- ProbeResult{Bitrate: pm.Bitrate}:
		default:
		}
	}
}

func (sess *Session) notifyResults(bitrate uint64) {
	select {
	case <-sess.probeResponseCh:
	default:
	}
	select {
	case sess.probeResponseCh <- ProbeResult{Bitrate: bitrate}:
	default:
	}
}

func (sess *Session) notifyTargets(bitrate uint64) {
	select {
	case <-sess.probeTargetsCh:
	default:
	}
	select {
	case sess.probeTargetsCh <- ProbeResult{Bitrate: bitrate}:
	default:
	}
}

func (sess *Session) detectBitrateChanges(provider probeStatsProvider) {
	sess.bitrateTracker.monitor(sess.ctx, sess.config.probeInterval(), provider, func(bitrate, rtt uint64) {
		sess.incomingProbeMu.Lock()
		stream := sess.incomingProbeStream
		sess.incomingProbeMu.Unlock()
		if stream == nil {
			return
		}

		err := message.ProbeMessage{
			Bitrate: bitrate,
			RTT:     rtt,
		}.Encode(stream)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				sess.logError("failed to send periodic probe", err)
			}
		}
	})
}

type probeStatsProvider interface {
	ConnectionStats() quic.ConnectionStats
}

type bitrateTracker struct {
	maxAge   time.Duration
	maxDelta float64

	// bitrate measurement state
	initialized bool
	bytesSent   uint64
	sampleTime  time.Time

	// throttle state
	estimatedBitrate atomic.Uint64
	lastSentBitrate  atomic.Uint64
	lastSentAt       time.Time
}

func (t *bitrateTracker) monitor(ctx context.Context, interval time.Duration, provider probeStatsProvider, onProbe func(bitrate, rtt uint64)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			stats := provider.ConnectionStats()
			bitrate, ok := t.next(stats, now)

			if !ok {
				continue
			}

			if onProbe != nil {
				onProbe(bitrate, uint64(stats.SmoothedRTT.Milliseconds()))
			}
		}
	}
}

func (t *bitrateTracker) next(stats quic.ConnectionStats, now time.Time) (uint64, bool) {
	bitrate := t.measureBitrate(stats, now)

	if t.lastSentAt.IsZero() {
		t.record(bitrate, now)
		return bitrate, true
	}

	lastSentBitrate := t.lastSentBitrate.Load()
	if now.Sub(t.lastSentAt) >= t.maxAge ||
		hasDelta(lastSentBitrate, bitrate, t.maxDelta) {
		t.record(bitrate, now)
		return bitrate, true
	}

	return bitrate, false
}

func (t *bitrateTracker) record(bitrate uint64, now time.Time) {
	t.estimatedBitrate.Store(bitrate)
	t.lastSentBitrate.Store(bitrate)
	t.lastSentAt = now
}

func (t *bitrateTracker) measureBitrate(stats quic.ConnectionStats, now time.Time) uint64 {
	if !t.initialized {
		t.initialized = true
		t.bytesSent = stats.BytesSent
		t.sampleTime = now
		return t.estimatedBitrate.Load()
	}

	elapsed := now.Sub(t.sampleTime)
	if elapsed <= 0 {
		return t.estimatedBitrate.Load()
	}

	bytesSent := stats.BytesSent
	var bytesDelta uint64
	if bytesSent >= t.bytesSent {
		bytesDelta = bytesSent - t.bytesSent
	}
	t.bytesSent = bytesSent
	t.sampleTime = now

	bitrate := uint64(float64(bytesDelta) * 8 / elapsed.Seconds())
	t.estimatedBitrate.Store(bitrate)
	return bitrate
}

func (t *bitrateTracker) getEstimatedBitrate() uint64 {
	return t.estimatedBitrate.Load()
}

func hasDelta(oldVal, newVal uint64, maxDelta float64) bool {
	if oldVal == 0 {
		return newVal != 0
	}
	var diff float64
	if newVal >= oldVal {
		diff = float64(newVal - oldVal)
	} else {
		diff = float64(oldVal - newVal)
	}
	return diff/float64(oldVal) >= maxDelta
}

func (sess *Session) handleGoawayStream(stream transport.Stream) error {
	var gm message.GoawayMessage
	err := gm.Decode(stream)
	if err != nil {
		return err
	}

	sess.isTerminating.Store(true)

	if sess.onGoaway != nil {
		sess.onGoaway(gm.NewSessionURI)
	}

	// Wait for the sender to FIN (close the send direction) indicating
	// the sender is ready to terminate the session.
	_, _ = io.Copy(io.Discard, stream)

	return nil
}
