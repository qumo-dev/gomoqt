package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_Path_ClientReturnsRequestPath(t *testing.T) {
	conn := &FakeStreamConn{}
	role := sessionRole{isClient: true, hasRequestURI: true, requestPath: "/live/alice"}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	path, err := sess.Path(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "/live/alice", path)
}

func TestSession_Path_ServerNativeQUIC_BlocksUntilSetup(t *testing.T) {
	conn := &FakeStreamConn{}
	// A native QUIC server: no request URI, so Path blocks on the peer SETUP.
	role := sessionRole{isClient: false, hasRequestURI: false}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	got := make(chan string, 1)
	go func() {
		p, _ := sess.Path(context.Background())
		got <- p
	}()

	// Not yet available.
	select {
	case <-got:
		t.Fatal("Path returned before SETUP")
	case <-time.After(20 * time.Millisecond):
	}

	// Deliver SETUP carrying the Path parameter. handleSetupStream expects the
	// stream positioned past the stream-type byte (processUniStream consumes it).
	var setupBuf bytes.Buffer
	var sm message.SetupMessage
	sm.AddPath("/origin/stream")
	require.NoError(t, sm.Encode(&setupBuf))

	stream := &FakeQUICReceiveStream{ReadFunc: bytes.NewReader(setupBuf.Bytes()).Read}
	sess.handleSetupStream(stream)

	select {
	case p := <-got:
		assert.Equal(t, "/origin/stream", p)
	case <-time.After(time.Second):
		t.Fatal("Path did not resolve after SETUP")
	}
}

func TestSession_Path_ServerNativeQUIC_CtxCanceled(t *testing.T) {
	conn := &FakeStreamConn{}
	role := sessionRole{isClient: false, hasRequestURI: false}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := sess.Path(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
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
	sess := newSession(conn, mux, nil, nil, nil, nil, nil, sessionRole{isClient: false, hasRequestURI: true})
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
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionRole{isClient: false, hasRequestURI: true})
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
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionRole{})
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

	sess := newSession(noStatsConn{}, mux, nil, nil, nil, nil, nil, sessionRole{})
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
