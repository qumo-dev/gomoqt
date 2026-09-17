package moqt

import (
	"bytes"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectSetupStream returns a FakeStreamConn whose OpenUniStreamSync hands
// out a send stream that records everything written, plus a wait function
// that blocks until the stream has been closed (FIN) and returns the bytes.
func collectSetupStream(tb testing.TB) (*FakeStreamConn, func() []byte) {
	tb.Helper()

	stream := &FakeQUICSendStream{}
	conn := &FakeStreamConn{
		OpenUniStreams: []sendStreamResult{
			{Stream: stream},
		},
	}

	wait := func() []byte {
		tb.Helper()
		select {
		case <-stream.Context().Done():
		case <-time.After(time.Second):
			tb.Fatal("timeout waiting for setup stream FIN")
		}
		return stream.Written()
	}

	return conn, wait
}

func decodeSetupBytes(tb testing.TB, data []byte) message.SetupMessage {
	tb.Helper()
	r := bytes.NewReader(data)

	var st message.StreamType
	require.NoError(tb, st.Decode(r))
	require.Equal(tb, message.StreamTypeSetup, st)

	var sm message.SetupMessage
	require.NoError(tb, sm.Decode(r))
	return sm
}

func TestSession_OpenSetupStream_QUICClientSendsPath(t *testing.T) {
	conn, wait := collectSetupStream(t)

	role := sessionSetup{path: "/live", sendPath: true}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role, nil)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	path, ok := sm.Path()
	assert.True(t, ok, "a native QUIC client must send the Path parameter")
	assert.Equal(t, "/live", path)
}

func TestSession_OpenSetupStream_WebTransportClientOmitsPath(t *testing.T) {
	conn, wait := collectSetupStream(t)

	// WebTransport: the path is known but sendPath is false → Path is omitted.
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil,
		sessionSetup{path: "/live"}, nil)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	_, ok := sm.Path()
	assert.False(t, ok, "the Path parameter is prohibited on a binding with a request URI")
	assert.Equal(t, "/live", sess.Path(), "the path is still observable via Session.Path")
}

func TestSession_OpenSetupStream_ServerOmitsPath(t *testing.T) {
	conn, wait := collectSetupStream(t)

	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{}, nil)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	_, ok := sm.Path()
	assert.False(t, ok, "a server must not send the Path parameter")
}

func TestSession_HandleSetupStream(t *testing.T) {
	// This handler only runs for sessions whose peer MUST NOT send a Path
	// parameter (WebTransport both roles; the native-QUIC client receiving the
	// server's SETUP). The native-QUIC server peer does send a Path, but that
	// stream is consumed by the router above Session — see the router tests in
	// session_draft05_test.go. So one uniform rule applies here: any Path
	// parameter is a protocol violation.
	tests := map[string]struct {
		setup        func() message.SetupMessage
		wantViolated bool
	}{
		"accepts empty setup": {
			setup: func() message.SetupMessage { return message.SetupMessage{} },
		},
		"accepts setup with probe only": {
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddProbe(message.ProbeLevelReport)
				return sm
			},
		},
		"rejects path parameter": {
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("/live")
				return sm
			},
			wantViolated: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conn := &FakeStreamConn{}
			sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{}, nil)
			defer sess.CloseWithError(NoError, "")

			var buf bytes.Buffer
			require.NoError(t, tt.setup().Encode(&buf))
			stream := &FakeQUICReceiveStream{Reads: []streamResult{{Data: buf.Bytes()}}}

			sess.handleSetupStream(stream)

			if tt.wantViolated {
				// terminateProtocolViolation closes the session on a fresh
				// goroutine; wait for the connection context to be canceled.
				select {
				case <-conn.Context().Done():
				case <-time.After(time.Second):
					t.Fatal("session was not terminated on protocol violation")
				}
				return
			}

			select {
			case <-sess.peerSetupCh:
			case <-time.After(time.Second):
				t.Fatal("peer setup was not recorded")
			}
			assert.NoError(t, conn.Context().Err(), "session must stay open")
		})
	}
}

func TestSession_HandleSetupStream_Duplicate(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{}, nil)
	defer sess.CloseWithError(NoError, "")

	encode := func() *FakeQUICReceiveStream {
		var buf bytes.Buffer
		require.NoError(t, message.SetupMessage{}.Encode(&buf))
		return &FakeQUICReceiveStream{Reads: []streamResult{{Data: buf.Bytes()}}}
	}

	sess.handleSetupStream(encode())
	select {
	case <-sess.peerSetupCh:
	case <-time.After(time.Second):
		t.Fatal("first setup was not recorded")
	}

	// A second Setup Stream is a protocol violation.
	sess.handleSetupStream(encode())
	select {
	case <-conn.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("session was not terminated on duplicate setup stream")
	}
}

func TestSession_Probe_PeerWithoutProbeCapability(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionSetup{}, nil)
	defer sess.CloseWithError(NoError, "")

	markPeerSetupReceived(sess, message.ProbeLevelNone)

	_, err := sess.Probe(1_000_000)
	assert.ErrorIs(t, err, ErrProbeNotSupported)
}
