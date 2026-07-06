package moqt

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectSetupStream returns a FakeStreamConn whose OpenUniStreamSync hands
// out a send stream that records everything written, plus a wait function
// that blocks until the stream has been closed (FIN) and returns the bytes.
func collectSetupStream(tb testing.TB) (*FakeStreamConn, func() []byte) {
	tb.Helper()

	var mu sync.Mutex
	var buf bytes.Buffer
	closed := make(chan struct{})

	stream := &FakeQUICSendStream{
		WriteFunc: func(p []byte) (int, error) {
			mu.Lock()
			defer mu.Unlock()
			return buf.Write(p)
		},
		CloseFunc: func() error {
			close(closed)
			return nil
		},
	}
	conn := &FakeStreamConn{
		OpenUniStreamSyncFunc: func(ctx context.Context) (transport.SendStream, error) {
			return stream, nil
		},
	}

	wait := func() []byte {
		tb.Helper()
		select {
		case <-closed:
		case <-time.After(time.Second):
			tb.Fatal("timeout waiting for setup stream FIN")
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), buf.Bytes()...)
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

	role := sessionRole{isClient: true, hasRequestURI: false, requestPath: "/live"}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	path, ok := sm.Path()
	assert.True(t, ok, "a native QUIC client must send the Path parameter")
	assert.Equal(t, "/live", path)
}

func TestSession_OpenSetupStream_WebTransportClientOmitsPath(t *testing.T) {
	conn, wait := collectSetupStream(t)

	role := sessionRole{isClient: true, hasRequestURI: true, requestPath: "/live"}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	_, ok := sm.Path()
	assert.False(t, ok, "the Path parameter is prohibited on a binding with a request URI")
}

func TestSession_OpenSetupStream_ServerOmitsPath(t *testing.T) {
	conn, wait := collectSetupStream(t)

	role := sessionRole{isClient: false, hasRequestURI: false}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, role)
	defer sess.CloseWithError(NoError, "")

	sm := decodeSetupBytes(t, wait())
	_, ok := sm.Path()
	assert.False(t, ok, "a server must not send the Path parameter")
}

func TestSession_HandleSetupStream(t *testing.T) {
	tests := map[string]struct {
		role         sessionRole
		setup        func() message.SetupMessage
		wantViolated bool
		wantPath     string
	}{
		"server on native QUIC accepts path": {
			role: sessionRole{isClient: false, hasRequestURI: false},
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("/live")
				return sm
			},
			wantPath: "/live",
		},
		"server on native QUIC rejects missing path": {
			role:         sessionRole{isClient: false, hasRequestURI: false},
			setup:        func() message.SetupMessage { return message.SetupMessage{} },
			wantViolated: true,
		},
		"server on native QUIC rejects invalid path": {
			role: sessionRole{isClient: false, hasRequestURI: false},
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("no-leading-slash")
				return sm
			},
			wantViolated: true,
		},
		"server with request URI rejects path": {
			role: sessionRole{isClient: false, hasRequestURI: true},
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("/live")
				return sm
			},
			wantViolated: true,
		},
		"client rejects path from server": {
			role: sessionRole{isClient: true, hasRequestURI: true},
			setup: func() message.SetupMessage {
				var sm message.SetupMessage
				sm.AddPath("/live")
				return sm
			},
			wantViolated: true,
		},
		"client accepts empty setup": {
			role:  sessionRole{isClient: true, hasRequestURI: true},
			setup: func() message.SetupMessage { return message.SetupMessage{} },
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			conn := &FakeStreamConn{}
			sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, tt.role)
			defer sess.CloseWithError(NoError, "")

			var buf bytes.Buffer
			require.NoError(t, tt.setup().Encode(&buf))
			stream := &FakeQUICReceiveStream{ReadFunc: buf.Read}

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
			if tt.wantPath != "" {
				assert.Equal(t, tt.wantPath, sess.peerPath)
			}
			assert.NoError(t, conn.Context().Err(), "session must stay open")
		})
	}
}

func TestSession_HandleSetupStream_Duplicate(t *testing.T) {
	conn := &FakeStreamConn{}
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionRole{isClient: true, hasRequestURI: true})
	defer sess.CloseWithError(NoError, "")

	encode := func() *FakeQUICReceiveStream {
		var buf bytes.Buffer
		require.NoError(t, message.SetupMessage{}.Encode(&buf))
		return &FakeQUICReceiveStream{ReadFunc: buf.Read}
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
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, sessionRole{})
	defer sess.CloseWithError(NoError, "")

	markPeerSetupReceived(sess, message.ProbeLevelNone)

	_, err := sess.Probe(1_000_000)
	assert.ErrorIs(t, err, ErrProbeNotSupported)
}
