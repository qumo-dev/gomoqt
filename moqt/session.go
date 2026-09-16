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
	"github.com/qumo-dev/gomoqt/transport"
)

const (
	moqtVersion = "moq-lite-05"
)

// sessionSetup carries the two pieces of binding-specific SETUP state that a
// Session cannot derive itself. Both fields are optional; the zero value is a
// WebTransport server (sends no Path, reads the peer SETUP itself).
type sessionSetup struct {
	// setupPath is the Path parameter this endpoint sends in its own outgoing
	// SETUP. Set only by the native-QUIC client (whose binding has no
	// handshake-time request URI); empty means "do not send Path".
	setupPath string
	// peerSetup is the peer's SETUP message, pre-decoded by the native-QUIC
	// router on the server side. The router consumes the client's Setup Stream
	// to learn the request path (above Session), then hands the decoded message
	// here so Session can seed its peer-probe state without re-reading the
	// stream. Nil means Session reads the peer SETUP itself (all other bindings).
	peerSetup *message.SetupMessage
}

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

	// setupPath is the Path sent in our outgoing SETUP (native-QUIC client only).
	setupPath string

	// localProbeLevel is the Probe capability advertised in our SETUP.
	localProbeLevel uint64

	// peerSetupReceived guards against duplicate Setup Streams.
	peerSetupReceived atomic.Bool
	// peerSetupCh is closed once the peer's SETUP message has been processed.
	// peerProbeLevel is written before the close and must only be read after it.
	peerSetupCh    chan struct{}
	peerProbeLevel uint64

	// probe stream state (subscriber side, lazily initialized)
	outgoingProbeMu     sync.Mutex
	outgoingProbeStream transport.Stream
	probeResponseCh     chan ProbeResult
	probeChannelsMu     sync.Mutex

	// incoming probe stream state (publisher side)
	incomingProbeMu     sync.Mutex
	incomingProbeStream transport.Stream
	probeTargetsCh      chan ProbeResult

	// counters optionally points to the Server accept pipeline counters.
	counters *ServerCounters

	bitrateTracker bitrateTracker

	probeMonitorOnce sync.Once // starts the bitrate monitor lazily (only when a probe stream arrives)
}

func newSession(
	conn StreamConn,
	mux *TrackMux,
	manager *connManager,
	config *Config,
	fetchHandler FetchHandler,
	onGoaway func(newSessionURI string),
	logger *slog.Logger,
	setup sessionSetup,
	counters *ServerCounters,
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
		setupPath:       setup.setupPath,
		trackReaders:    make(map[SubscribeID]*TrackReader),
		trackWriters:    make(map[SubscribeID]*TrackWriter),
		connManager:     manager,
		peerSetupCh:     make(chan struct{}),
		probeResponseCh: make(chan ProbeResult, 1), // latest-value semantics
		probeTargetsCh:  make(chan ProbeResult, 1), // latest-value semantics
		counters:        counters,
	}

	// When the native-QUIC router has already consumed the peer's Setup Stream
	// (to learn the request path above Session), seed the peer-probe state from
	// the decoded message and mark setup received. Done before any goroutine is
	// launched so handleSetupStream's duplicate-guard sees it, and Probe() does
	// not block on waitPeerSetup.
	if setup.peerSetup != nil {
		sess.peerProbeLevel = setup.peerSetup.ProbeLevel()
		sess.peerSetupReceived.Store(true)
		close(sess.peerSetupCh)
	}

	if manager != nil {
		manager.addConn(conn)
	}

	provider, _ := conn.(probeStatsProvider)
	sess.bitrateTracker = newBitrateTracker(config, provider)
	if provider != nil {
		// The bitrate tracker can measure and report the current sending
		// rate, so advertise the Report capability in SETUP.
		sess.localProbeLevel = message.ProbeLevelReport
	}

	// Advertise capabilities on the mandatory Setup Stream.
	sess.wg.Go(func() {
		sess.openSetupStream()
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

// openSetupStream sends this endpoint's SETUP message on a unidirectional
// Setup Stream and closes it (FIN), per moq-lite-05. A client on a binding
// without a request URI (native QUIC) includes the Path parameter.
func (sess *Session) openSetupStream() {
	stream, err := sess.conn.OpenUniStreamSync(sess.ctx)
	if err != nil {
		return
	}

	if err := message.StreamTypeSetup.Encode(stream); err != nil {
		stream.CancelWrite(transport.StreamErrorCode(InternalSessionErrorCode))
		return
	}

	var sm message.SetupMessage
	if sess.localProbeLevel != message.ProbeLevelNone {
		sm.AddProbe(sess.localProbeLevel)
	}
	// Only the native-QUIC client conveys a request path via SETUP; its
	// binding has no handshake-time request URI. setupPath is empty for every
	// other role (WebTransport both directions; native-QUIC server).
	if path := sess.setupPath; path != "" {
		sm.AddPath(path)
	}

	if err := sm.Encode(stream); err != nil {
		stream.CancelWrite(transport.StreamErrorCode(InternalSessionErrorCode))
		return
	}

	_ = stream.Close()
}

// handleSetupStream processes the peer's SETUP message. A second Setup
// Stream, a malformed message, or a Path parameter is a protocol violation
// that terminates the session.
//
// This handler runs only for sessions whose peer MUST NOT send a Path
// parameter (WebTransport in both roles, and the native-QUIC client receiving
// the server's SETUP). The native-QUIC server peer does send a Path, but that
// Setup Stream is consumed by the router above Session before this handler is
// ever reached — so a single uniform rule applies here: any Path parameter is
// a violation.
func (sess *Session) handleSetupStream(stream transport.ReceiveStream) {
	if !sess.peerSetupReceived.CompareAndSwap(false, true) {
		sess.terminateProtocolViolation("duplicate setup stream")
		return
	}

	var sm message.SetupMessage
	if err := sm.Decode(stream); err != nil {
		sess.logError("failed to decode SETUP message", err)
		sess.terminateProtocolViolation("malformed SETUP message")
		return
	}

	// For every binding that reaches this handler the peer is prohibited from
	// sending a Path parameter (the native-QUIC server case is handled by the
	// router, which never lets the stream reach here).
	if _, hasPath := sm.Path(); hasPath {
		sess.terminateProtocolViolation("unexpected Path parameter")
		return
	}

	sess.peerProbeLevel = sm.ProbeLevel()
	close(sess.peerSetupCh)
}

// terminateProtocolViolation closes the session with PROTOCOL_VIOLATION.
// It must be called from stream handlers, which run on the session WaitGroup;
// CloseWithError joins that WaitGroup, so it is invoked on a fresh goroutine.
func (sess *Session) terminateProtocolViolation(msg string) {
	go func() {
		_ = sess.CloseWithError(ProtocolViolationErrorCode, msg)
	}()
}

// waitPeerSetup blocks until the peer's SETUP has been received, the
// session terminates, or the setup timeout elapses.
func (sess *Session) waitPeerSetup() error {
	select {
	case <-sess.peerSetupCh:
		return nil
	default:
	}

	timer := time.NewTimer(sess.config.setupTimeout())
	defer timer.Stop()

	select {
	case <-sess.peerSetupCh:
		return nil
	case <-timer.C:
		return errors.New("timed out waiting for peer SETUP")
	case <-sess.ctx.Done():
		return Cause(sess.ctx)
	}
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

	// Always remove the conn from the manager exactly once, even if the
	// underlying conn.CloseWithError fails (e.g. the peer already closed it).
	// Without this, a peer-closed session leaks in the connManager and
	// Server.Close()/Shutdown() hang on <-connManager.Done().
	if s.connManager != nil {
		connManager := s.connManager
		s.connManager = nil
		defer connManager.removeConn(s.conn)
	}

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

	s.probeChannelsMu.Lock()
	close(s.probeResponseCh)
	close(s.probeTargetsCh)
	s.probeChannelsMu.Unlock()

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

	stream, err := s.conn.OpenStreamSync(ctx)
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
		GroupStart:           groupSequenceToWire(config.StartGroup),
		GroupEnd:             groupSequenceToWire(config.EndGroup),
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

	substr := newSendSubscribeStream(id, stream, config)

	track := newTrackReader(path, name, substr, func() { s.removeTrackReader(id) })
	s.addTrackReader(id, track)
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetReadDeadline(deadline)
		defer stream.SetReadDeadline(time.Time{})
	}

	resp, err := readSubscribeResponse(stream)
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

	switch {
	case resp.ok != nil:
		// SUBSCRIBE_OK resolves the absolute start group.
		substr.setResolvedStart(GroupSequence(resp.ok.Group))
	case resp.end != nil:
		// SUBSCRIBE_END without a preceding SUBSCRIBE_OK: the track has
		// already ended with no matching groups.
		substr.setEnd(GroupSequence(resp.end.Group))
	default:
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("moqt: unexpected SUBSCRIBE_DROP message before SUBSCRIBE_OK")
	}
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

	stream, err := s.conn.OpenStreamSync(s.ctx)
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open stream for fetch: %w", err)
	}

	// A fetch delivers a single group on its own stream, so there is no group
	// ordering to express.
	stream.SetPriority(urgencyFor(req.Priority, false))

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

	stream, err := sess.conn.OpenStreamSync(sess.ctx)
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

	err = message.AnnounceRequestMessage{
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

		return nil, fmt.Errorf("failed to send ANNOUNCE_REQUEST message: %w", err)
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

	// The publisher advertises its Probe capability in SETUP; a subscriber
	// MUST consult it before relying on a Probe Stream.
	if err := sess.waitPeerSetup(); err != nil {
		return nil, err
	}
	if sess.peerProbeLevel == message.ProbeLevelNone {
		return nil, ErrProbeNotSupported
	}

	sess.outgoingProbeMu.Lock()
	defer sess.outgoingProbeMu.Unlock()

	probeStream := sess.outgoingProbeStream
	// Lazily open the probe stream.
	if probeStream == nil || probeStream.Context().Err() != nil {
		stream, err := sess.conn.OpenStreamSync(sess.ctx)
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

				sess.notifyProbe(sess.probeResponseCh, ProbeResult{Bitrate: pm.Bitrate})

				select {
				case <-streamCtx.Done():
					return
				default:
				}
			}
		})

		probeStream = stream
	}

	// Send PROBE with the new target bitrate. Per moq-lite-05 the subscriber MAY send
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

		if sess.counters != nil {
			sess.counters.BiStreamAccepts.Add(1)
		}

		// Handle the stream. Tracked on sess.wg so CloseWithError joins in-flight
		// stream handlers before closing the probe channels (avoids send-on-close races).
		sess.wg.Go(func() {
			sess.processBiStream(stream)
		})
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

	switch streamType {
	case message.StreamTypeAnnounce:
		sess.handleAnnounceStream(stream)
	case message.StreamTypeSubscribe:
		sess.handleSubscribeStream(stream)
	case message.StreamTypeFetch:
		sess.handleFetchStream(stream)
	case message.StreamTypeTrack:
		sess.handleTrackStream(stream)
	case message.StreamTypeProbe:
		if sess.localProbeLevel == message.ProbeLevelNone {
			// We did not advertise the Probe capability; the spec requires
			// resetting a Probe Stream we cannot serve.
			cancelStreamWithError(stream, transport.StreamErrorCode(ProbeErrorCodeNotSupported))
			return
		}
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

// handleAnnounceStream decodes an ANNOUNCE_REQUEST and serves announcements on a
// new announcement writer backed by the stream.
func (sess *Session) handleAnnounceStream(stream transport.Stream) {
	var aim message.AnnounceRequestMessage
	err := aim.Decode(stream)
	if err != nil {
		sess.logError("failed to decode ANNOUNCE_REQUEST message", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(AnnounceErrorCodeInternal))
		return
	}

	prefix := aim.BroadcastPathPrefix

	annstr := newAnnouncementWriter(stream, prefix, sess.mux.hopID, aim.ExcludeHop, sess.logger)

	sess.mux.serveAnnouncements(annstr)

	// Ensure the announcement writer is closed when done
	annstr.Close()
}

// handleSubscribeStream decodes a SUBSCRIBE and registers a track writer for the
// incoming track. Group streams are opened via OpenUniStreamSync so the publisher
// backpressures on the peer's uni-stream limit instead of aborting (see #211).
func (sess *Session) handleSubscribeStream(stream transport.Stream) {
	var sm message.SubscribeMessage
	err := sm.Decode(stream)
	if err != nil {
		if sess.counters != nil {
			sess.counters.SubscribeErrors.Add(1)
		}
		sess.logError("failed to decode SUBSCRIBE message", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return
	}

	if sess.counters != nil {
		sess.counters.SubscribesReceived.Add(1)
	}
	config := &SubscribeConfig{
		Priority:   TrackPriority(sm.SubscriberPriority),
		Ordered:    boolFromWireFlag(sm.SubscriberOrdered),
		MaxLatency: sm.SubscriberMaxLatency,
	}

	// Decode 0-sentinel / +1-encoded fields (matching SUBSCRIBE_UPDATE logic)
	config.StartGroup = groupSequenceFromWire(sm.GroupStart)
	config.EndGroup = groupSequenceFromWire(sm.GroupEnd)

	substr := newReceiveSubscribeStream(SubscribeID(sm.SubscribeID), stream, config)

	track := newTrackWriter(
		BroadcastPath(sm.BroadcastPath),
		TrackName(sm.TrackName),
		substr,
		// Backpressure: OpenUniStreamSync blocks until the peer grants a uni
		// stream (MAX_STREAMS) instead of returning a stream-limit error.
		// Without this, a publisher opening groups faster than the peer
		// recycles streams hit StreamLimitReachedError and aborted the whole
		// track, idling the connection (payload-1K/fpg-1 stream-churn repro).
		// The ctx handed to OpenGroup/OpenGroupAt flows through for cancellation.
		sess.conn.OpenUniStreamSync,
		func() { sess.removeTrackWriter(SubscribeID(sm.SubscribeID)) },
	)
	sess.addTrackWriter(SubscribeID(sm.SubscribeID), track)

	if sess.counters != nil {
		sess.counters.SubscribesServed.Add(1)
	}

	sess.mux.serveTrack(track)

	// Ensure the track writer is closed when done
	track.Close()
}

// handleTrackStream decodes a TRACK message and responds with the track's
// immutable publisher properties in a single TRACK_INFO message, then FINs
// the stream (via the deferred Close in processBiStream). Unknown tracks
// reset the stream.
func (sess *Session) handleTrackStream(stream transport.Stream) {
	var tm message.TrackMessage
	if err := tm.Decode(stream); err != nil {
		sess.logError("failed to decode TRACK message", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return
	}

	ann, handler := sess.mux.TrackHandler(BroadcastPath(tm.BroadcastPath))
	if ann == nil {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeNotFound))
		return
	}

	var info PublishInfo
	if provider, ok := handler.(TrackInfoProvider); ok {
		i, found := provider.TrackInfo(TrackName(tm.TrackName))
		if !found {
			cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeNotFound))
			return
		}
		info = i
	}

	err := message.TrackInfoMessage{
		PublisherPriority:   uint8(info.Priority),
		PublisherOrdered:    boolToWireFlag(info.Ordered),
		PublisherMaxLatency: info.MaxLatency,
		Timescale:           info.timescaleOrDefault(),
	}.Encode(stream)
	if err != nil {
		sess.logError("failed to encode TRACK_INFO message", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return
	}
}

// TrackInfo opens a Track Stream and requests the immutable publisher
// properties of a track (TRACK_INFO), including the Timescale needed to
// interpret frame timestamps. The returned properties are fixed for the
// lifetime of the track and SHOULD be cached by the caller.
func (sess *Session) TrackInfo(ctx context.Context, path BroadcastPath, name TrackName) (*PublishInfo, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}

	if sess.terminating() {
		return nil, ErrClosedSession
	}

	if !isValidPath(path) {
		return nil, fmt.Errorf("invalid broadcast path: %q", path)
	}

	stream, err := sess.conn.OpenStreamSync(ctx)
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{
				ApplicationError: appErr,
			}
		}
		return nil, fmt.Errorf("failed to open stream for track info: %w", err)
	}

	err = message.StreamTypeTrack.Encode(stream)
	if err != nil {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to encode stream type message: %w", err)
	}

	err = message.TrackMessage{
		BroadcastPath: string(path),
		TrackName:     string(name),
	}.Encode(stream)
	if err != nil {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to encode TRACK message: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetReadDeadline(deadline)
		defer stream.SetReadDeadline(time.Time{})
	}

	var tim message.TrackInfoMessage
	err = tim.Decode(stream)
	if err != nil {
		if strErr, ok := errors.AsType[*transport.StreamError](err); ok {
			return nil, &SubscribeError{StreamError: strErr}
		}
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("failed to read TRACK_INFO message: %w", err)
	}

	_ = stream.Close()

	if tim.Timescale == 0 {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, errors.New("moqt: received TRACK_INFO with zero Timescale")
	}

	return &PublishInfo{
		Priority:   TrackPriority(tim.PublisherPriority),
		Ordered:    boolFromWireFlag(tim.PublisherOrdered),
		MaxLatency: tim.PublisherMaxLatency,
		Timescale:  tim.Timescale,
	}, nil
}

// handleFetchStream decodes a FETCH and dispatches it to the configured fetch handler.
func (sess *Session) handleFetchStream(stream transport.Stream) {
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

	// Priority is per-endpoint and not negotiated, so the requester's call on
	// its own end does not cover the response data written from this side.
	// A fetch delivers a single group on its own stream, so there is no group
	// ordering to express.
	stream.SetPriority(urgencyFor(req.Priority, false))

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
}

func (sess *Session) handleUniStreams() {
	for {
		stream, err := sess.conn.AcceptUniStream(sess.ctx)
		if err != nil {
			return
		}

		sess.wg.Go(func() {
			sess.processUniStream(stream)
		})
	}
}

func (sess *Session) processUniStream(stream transport.ReceiveStream) {
	var streamType message.StreamType
	err := streamType.Decode(stream)
	if err != nil {
		sess.logError("failed to decode uni stream type", err)
		return
	}

	switch streamType {
	case message.StreamTypeSetup:
		sess.handleSetupStream(stream)
	case message.StreamTypeGroup:
		var gm message.GroupMessage
		err := gm.Decode(stream)
		if err != nil {
			sess.logError("failed to decode GROUP message", err)
			return
		}

		sess.trackReaderMapLocker.RLock()
		track, ok := sess.trackReaders[SubscribeID(gm.SubscribeID)]
		sess.trackReaderMapLocker.RUnlock()
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

	// Lazily start the bitrate monitor the first time a peer opens a probe stream.
	sess.startProbeMonitorOnce()

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

		sess.notifyProbe(sess.probeTargetsCh, ProbeResult{Bitrate: pm.Bitrate})
	}
}

func (sess *Session) notifyResults(bitrate uint64) {
	sess.notifyProbe(sess.probeResponseCh, ProbeResult{Bitrate: bitrate})
}

func (sess *Session) notifyProbe(ch chan ProbeResult, result ProbeResult) {
	sess.probeChannelsMu.Lock()
	defer sess.probeChannelsMu.Unlock()
	if sess.terminating() {
		return
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- result:
	default:
	}
}

func (sess *Session) notifyTargets(bitrate uint64) {
	sess.notifyProbe(sess.probeTargetsCh, ProbeResult{Bitrate: bitrate})
}

// startProbeMonitorOnce starts the bitrate monitor goroutine the first time a
// peer opens a probe stream to this session. Sessions that are never probed
// (the common subscriber case in high-fan-out) never start it — one fewer
// goroutine per session, preserving EstimatedBitrate via lazy Stats() sampling.
func (sess *Session) startProbeMonitorOnce() {
	sess.probeMonitorOnce.Do(func() {
		if sess.bitrateTracker.provider != nil {
			sess.bitrateTracker.monitorRunning.Store(true)
			sess.wg.Go(func() { sess.detectBitrateChanges(sess.bitrateTracker.provider) })
		}
	})
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

	mu       sync.Mutex         // guards non-atomic fields (initialized, bytesSent, sampleTime, lastSentAt)
	provider probeStatsProvider // connection-stats source; nil if the conn exposes none

	// monitorRunning is set once the background monitor goroutine starts (a
	// probe stream arrived). While set, the monitor owns the sampling baseline
	// and keeps estimatedBitrate fresh, so getEstimatedBitrate reads it
	// passively instead of sampling — otherwise a Stats() call would consume the
	// monitor's byte-delta window and understate its next writeback to the prober.
	monitorRunning atomic.Bool
}

// newBitrateTracker builds a tracker for a session. provider is nil when the
// connection exposes no stats (some WebTransport conns, test fakes); such a
// tracker stays inert — EstimatedBitrate stays zero and the monitor never runs.
func newBitrateTracker(config *Config, provider probeStatsProvider) bitrateTracker {
	return bitrateTracker{
		maxAge:   config.probeMaxAge(),
		maxDelta: config.probeMaxDelta(),
		provider: provider,
	}
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

// next takes one bitrate sample. It returns the measured bitrate and whether
// to notify the prober (first sample, maxAge elapsed, or a large-enough
// delta). measureBitrate already stored estimatedBitrate, so notifying only
// advances the throttle bookkeeping that gates how often we write back to the
// prober.
func (t *bitrateTracker) next(stats quic.ConnectionStats, now time.Time) (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	bitrate := t.measureBitrate(stats, now)

	notify := t.lastSentAt.IsZero() ||
		now.Sub(t.lastSentAt) >= t.maxAge ||
		hasDelta(t.lastSentBitrate.Load(), bitrate, t.maxDelta)
	if notify {
		t.lastSentBitrate.Store(bitrate)
		t.lastSentAt = now
	}
	return bitrate, notify
}

// record stores a bitrate sample from outside the monitor loop (e.g. a
// peer-reported PROBE result) and locks t.mu itself, so callers need not.
func (t *bitrateTracker) record(bitrate uint64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
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
	// When the monitor goroutine is running it owns the sampling baseline and
	// keeps estimatedBitrate fresh; read it passively so Stats() does not consume
	// the monitor's byte-delta window. This also covers the nil-provider case
	// (no monitor, no lazy sampling — estimatedBitrate stays whatever it was).
	if t.provider == nil || t.monitorRunning.Load() {
		return t.estimatedBitrate.Load()
	}
	// Lazy sampling: no monitor is running (a never-probed session), so compute
	// EstimatedBitrate on demand from local connection stats — this replaces the
	// eager background monitor for subscribers. Stats() is intended to be called
	// at monitoring cadence, not per-frame: each call samples over the window
	// elapsed since the last call, so very frequent polling yields noisy values.
	now := time.Now()
	stats := t.provider.ConnectionStats()
	t.mu.Lock()
	defer t.mu.Unlock()
	// measureBitrate updates estimatedBitrate directly. We deliberately do NOT
	// call record() here: that would also mutate the monitor's probe-writeback
	// throttle (lastSentAt/lastSentBitrate) and suppress responses to probers,
	// which Stats() has no business touching.
	return t.measureBitrate(stats, now)
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
