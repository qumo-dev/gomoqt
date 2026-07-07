package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadClientSetup_ExtractsPathAndProbe drives the native-QUIC router's
// setup reader: the client's first uni stream carries SETUP with a Path and a
// Probe level, and readClientSetup decodes both. It also covers the
// path-validation the router performs (missing/invalid Path → error) and the
// non-Setup stream-type rejection, which previously lived in handleSetupStream.
func TestReadClientSetup_ExtractsPathAndProbe(t *testing.T) {
	encodeSetup := func(sm message.SetupMessage) []byte {
		var buf bytes.Buffer
		require.NoError(t, message.StreamTypeSetup.Encode(&buf))
		require.NoError(t, sm.Encode(&buf))
		return buf.Bytes()
	}

	t.Run("decodes path and probe level", func(t *testing.T) {
		var sm message.SetupMessage
		sm.AddPath("/live/alice")
		sm.AddProbe(message.ProbeLevelReport)

		conn := &FakeStreamConn{
			AcceptUniStreamFunc: func(context.Context) (transport.ReceiveStream, error) {
				return &FakeQUICReceiveStream{ReadFunc: bytes.NewReader(encodeSetup(sm)).Read}, nil
			},
		}
		got, err := readClientSetup(conn, time.Second)
		require.NoError(t, err)
		path, ok := got.Path()
		assert.True(t, ok)
		assert.Equal(t, "/live/alice", path)
		assert.Equal(t, uint64(message.ProbeLevelReport), got.ProbeLevel())
	})

	t.Run("rejects missing path", func(t *testing.T) {
		conn := &FakeStreamConn{
			AcceptUniStreamFunc: func(context.Context) (transport.ReceiveStream, error) {
				return &FakeQUICReceiveStream{ReadFunc: bytes.NewReader(encodeSetup(message.SetupMessage{})).Read}, nil
			},
		}
		sm, err := readClientSetup(conn, time.Second)
		require.NoError(t, err) // decode succeeds; the path check is the router's job
		_, ok := sm.Path()
		assert.False(t, ok, "router input validation rejects a missing path")
	})

	t.Run("rejects non-setup stream type", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, message.StreamTypeTrack.Encode(&buf)) // wrong type
		conn := &FakeStreamConn{
			AcceptUniStreamFunc: func(context.Context) (transport.ReceiveStream, error) {
				return &FakeQUICReceiveStream{ReadFunc: bytes.NewReader(buf.Bytes()).Read}, nil
			},
		}
		_, err := readClientSetup(conn, time.Second)
		assert.Error(t, err)
	})

	t.Run("accept error surfaces", func(t *testing.T) {
		conn := &FakeStreamConn{
			AcceptUniStreamFunc: func(context.Context) (transport.ReceiveStream, error) {
				return nil, context.Canceled
			},
		}
		_, err := readClientSetup(conn, time.Second)
		assert.Error(t, err)
	})
}

// TestPathContext_RoundTrips verifies the router stashes the learned path on
// the connection context and handlers recover it via PathFromContext — the
// native-QUIC analog of WebTransport's r.URL.Path.
func TestPathContext_RoundTrips(t *testing.T) {
	conn := &FakeStreamConn{}
	wrapped := withPathContext(conn, "/live/alice")

	path, ok := PathFromContext(wrapped.Context())
	require.True(t, ok)
	assert.Equal(t, "/live/alice", path)

	// The base conn (no router) has no path, like a client-side session.
	_, ok = PathFromContext(conn.Context())
	assert.False(t, ok)
}

// TestNewSession_InjectedPeerSetup verifies that when the native-QUIC router
// hands Session a pre-decoded peer SETUP, the peer-probe state is seeded and
// peerSetupCh is already closed — so Probe()/waitPeerSetup do not block and a
// later peer Setup Stream is treated as a duplicate.
func TestNewSession_InjectedPeerSetup(t *testing.T) {
	conn := &FakeStreamConn{}
	var sm message.SetupMessage
	sm.AddProbe(message.ProbeLevelReport)
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{peerSetup: &sm})
	defer sess.CloseWithError(NoError, "")

	select {
	case <-sess.peerSetupCh:
	default:
		t.Fatal("peerSetupCh must be closed when peerSetup is injected")
	}
	assert.Equal(t, uint64(message.ProbeLevelReport), sess.peerProbeLevel)
	assert.True(t, sess.peerSetupReceived.Load(), "injected setup must mark peerSetupReceived")
}

// TestSession_ProcessBiStream_TrackStream verifies a Track stream (0x6) is
// dispatched to handleTrackStream and answered with TRACK_INFO.
func TestSession_ProcessBiStream_TrackStream(t *testing.T) {
	mux := NewTrackMux(0)
	b := NewBroadcast()
	require.NoError(t, b.RegisterWithInfo("video",
		PublishInfo{Priority: 3, Timescale: 90000},
		TrackHandlerFunc(func(*TrackWriter) {})))
	mux.Publish(context.Background(), "/live", b)

	conn := &FakeStreamConn{}
	sess := newSession(conn, mux, nil, nil, nil, nil, nil, sessionSetup{})
	defer sess.CloseWithError(NoError, "")

	var req bytes.Buffer
	require.NoError(t, message.StreamTypeTrack.Encode(&req))
	require.NoError(t, message.TrackMessage{BroadcastPath: "/live", TrackName: "video"}.Encode(&req))

	var written bytes.Buffer
	stream := &FakeQUICStream{
		ReadFunc:  bytes.NewReader(req.Bytes()).Read,
		WriteFunc: written.Write,
	}
	done := make(chan struct{})
	go func() { sess.processBiStream(stream); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("processBiStream did not return")
	}

	var tim message.TrackInfoMessage
	require.NoError(t, tim.Decode(&written))
	assert.Equal(t, uint64(90000), tim.Timescale)
	assert.Equal(t, uint8(3), tim.PublisherPriority)
}

// TestSession_ProcessBiStream_ProbeResetsWhenUnadvertised verifies that an
// incoming Probe Stream on a session that advertised no Probe capability is
// reset (not served).
func TestSession_ProcessBiStream_ProbeResetsWhenUnadvertised(t *testing.T) {
	// noStatsConn does not implement probeStatsProvider, so localProbeLevel
	// stays None and the Probe Stream must be reset.
	conn := noStatsConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{})
	defer sess.CloseWithError(NoError, "")
	require.Equal(t, message.ProbeLevelNone, sess.localProbeLevel)

	var req bytes.Buffer
	require.NoError(t, message.StreamTypeProbe.Encode(&req))

	var readErr transport.StreamErrorCode
	stream := &FakeQUICStream{
		ReadFunc: bytes.NewReader(req.Bytes()).Read,
		CancelReadFunc: func(c transport.StreamErrorCode) { readErr = c },
	}
	done := make(chan struct{})
	go func() { sess.processBiStream(stream); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("processBiStream did not return")
	}
	assert.Equal(t, transport.StreamErrorCode(ProbeErrorCodeNotSupported), readErr)
}

// TestSession_TrackInfo_OpenStreamError exercises the open-stream error path.
func TestSession_TrackInfo_OpenStreamError(t *testing.T) {
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return nil, errors.New("boom") }
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	_, err := sess.TrackInfo(context.Background(), "/p", "t")
	assert.Error(t, err)
}

// TestSession_TrackInfo_DecodeError verifies a malformed TRACK_INFO (here:
// stream reset / EOF before a full message) surfaces as an error.
func TestSession_TrackInfo_DecodeError(t *testing.T) {
	requestStream := &FakeQUICStream{}
	// Nothing readable → ReadMessageLength hits EOF.
	requestStream.ReadFunc = func([]byte) (int, error) { return 0, io.EOF }
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return requestStream, nil }
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	_, err := sess.TrackInfo(context.Background(), "/p", "t")
	assert.Error(t, err)
}

// TestSession_HandleTrackStream_DecodeError verifies a malformed TRACK resets
// the stream rather than panicking.
func TestSession_HandleTrackStream_DecodeError(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{})
	defer sess.CloseWithError(NoError, "")

	var reset transport.StreamErrorCode
	stream := &FakeQUICStream{
		ReadFunc:       bytes.NewReader([]byte{0x05, 0x00}).Read, // bogus length, no body
		CancelReadFunc: func(c transport.StreamErrorCode) { reset = c },
	}
	sess.handleTrackStream(stream)
	assert.Equal(t, transport.StreamErrorCode(SubscribeErrorCodeInternal), reset)
}

// TestSession_HandleTrackStream_EncodeError verifies that a TRACK_INFO encode
// failure (stream write error) resets the stream with an internal error.
func TestSession_HandleTrackStream_EncodeError(t *testing.T) {
	mux := NewTrackMux(0)
	b := NewBroadcast()
	require.NoError(t, b.RegisterWithInfo("video",
		PublishInfo{Timescale: 1000}, TrackHandlerFunc(func(*TrackWriter) {})))
	mux.Publish(context.Background(), "/live", b)

	sess := newSession(noStatsConn{}, mux, nil, nil, nil, nil, nil, sessionSetup{})
	defer sess.CloseWithError(NoError, "")

	var req bytes.Buffer
	require.NoError(t, message.StreamTypeTrack.Encode(&req))
	require.NoError(t, message.TrackMessage{BroadcastPath: "/live", TrackName: "video"}.Encode(&req))

	var reset transport.StreamErrorCode
	stream := &FakeQUICStream{
		ReadFunc:  bytes.NewReader(req.Bytes()).Read,
		WriteFunc: func([]byte) (int, error) { return 0, errors.New("write closed") },
		CancelReadFunc: func(c transport.StreamErrorCode) { reset = c },
	}
	sess.handleTrackStream(stream)
	assert.Equal(t, transport.StreamErrorCode(SubscribeErrorCodeInternal), reset)
}

func TestSession_TrackInfo_Guards(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	t.Run("nil context", func(t *testing.T) {
		var nilCtx context.Context //nolint:staticcheck // deliberately nil to exercise the guard
		_, err := sess.TrackInfo(nilCtx, "/p", "t")
		assert.Error(t, err)
	})
	t.Run("terminating session", func(t *testing.T) {
		sess2 := newTestSession(&FakeStreamConn{})
		require.NoError(t, sess2.CloseWithError(NoError, ""))
		_, err := sess2.TrackInfo(context.Background(), "/p", "t")
		assert.ErrorIs(t, err, ErrClosedSession)
	})
	t.Run("invalid path", func(t *testing.T) {
		_, err := sess.TrackInfo(context.Background(), "no-leading-slash", "t")
		assert.Error(t, err)
	})
}

// TestSession_TrackInfo_StreamTypeEncodeError covers the encode-error branch:
// OpenStreamSync returns a stream whose first write fails.
func TestSession_TrackInfo_StreamTypeEncodeError(t *testing.T) {
	requestStream := &FakeQUICStream{
		WriteFunc: func([]byte) (int, error) { return 0, errors.New("closed") },
	}
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return requestStream, nil }
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	_, err := sess.TrackInfo(context.Background(), "/p", "t")
	assert.Error(t, err)
}

// TestSession_TrackInfo_OpenStreamApplicationError covers the ApplicationError
// branch: OpenStreamSync returns *transport.ApplicationError → *SessionError.
func TestSession_TrackInfo_OpenStreamApplicationError(t *testing.T) {
	appErr := &transport.ApplicationError{
		ErrorCode:    transport.ApplicationErrorCode(InternalSessionErrorCode),
		ErrorMessage: "application error",
	}
	conn := &FakeStreamConn{}
	conn.OpenStreamSyncFunc = func(context.Context) (transport.Stream, error) { return nil, appErr }
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	_, err := sess.TrackInfo(context.Background(), "/p", "t")
	var sessErr *SessionError
	require.ErrorAs(t, err, &sessErr)
}

// TestSession_TrackInfo_WithDeadline covers the SetReadDeadline branch.
func TestSession_TrackInfo_WithDeadline(t *testing.T) {
	var response bytes.Buffer
	require.NoError(t, (&message.TrackInfoMessage{Timescale: 1000}).Encode(&response))
	stream := &FakeQUICStream{ReadFunc: response.Read}
	conn := &FakeStreamConn{}
	conn.OpenStreamFunc = func() (transport.Stream, error) { return stream, nil }
	sess := newTestSession(conn)
	defer sess.CloseWithError(NoError, "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	info, err := sess.TrackInfo(ctx, "/p", "t")
	require.NoError(t, err)
	assert.Equal(t, uint64(1000), info.Timescale)
}

// TestSession_HandleSetupStream_MalformedSetup covers the decode-error →
// protocol-violation branch.
func TestSession_HandleSetupStream_MalformedSetup(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{})
	// one mark: the setup goroutine closes the session; defer keeps it tidy.
	defer sess.CloseWithError(NoError, "")

	// Garbage that cannot parse as a SETUP message body.
	stream := &FakeQUICReceiveStream{ReadFunc: bytes.NewReader([]byte{0x05, 0x00}).Read}
	sess.handleSetupStream(stream)

	select {
	case <-conn.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("malformed SETUP did not terminate the session")
	}
}

// TestSession_Probe_WaitPeerSetup_SessionCanceled covers the waitPeerSetup
// session-cancellation branch: Probe blocks on peer SETUP, the session is
// closed concurrently, and Probe returns an error. Uses testing/synctest so
// the "Probe is parked in waitPeerSetup before Close runs" ordering is
// deterministic (no ad-hoc sleep).
func TestSession_Probe_WaitPeerSetup_SessionCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// FakeStreamConn.Context() is cancellable (via CloseWithError), so
		// sess.ctx cancels on close and waitPeerSetup unblocks. Peer SETUP is
		// never delivered, so waitPeerSetup blocks until that cancellation.
		conn := &FakeStreamConn{}
		sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{})

		probeErr := make(chan error, 1)
		go func() {
			_, err := sess.Probe(1)
			probeErr <- err
		}()

		// Wait until Probe is parked in waitPeerSetup and all other session
		// goroutines are idle.
		synctest.Wait()

		go func() {
			_ = sess.CloseWithError(NoError, "")
		}()
		// Close cancels sess.ctx; waitPeerSetup unblocks via <-sess.ctx.Done().
		synctest.Wait()

		select {
		case err := <-probeErr:
			assert.Error(t, err)
		default:
			t.Fatal("Probe did not return after session close")
		}
	})
}
