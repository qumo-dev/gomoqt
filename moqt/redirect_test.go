package moqt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackWriter_Redirect(t *testing.T) {
	follows := func(context.Context, string) error { return nil }
	refuses := func(context.Context, string) error { return ErrRedirectNotSupported }

	tests := map[string]struct {
		allowed func(context.Context, string) error
		okSent  bool
		to      Redirect
		want    *message.SubscribeRedirectMessage
		wantErr error
	}{
		"keeps the subscriber's track name": {
			allowed: follows,
			to:      Redirect{BroadcastPath: "/real"},
			want:    &message.SubscribeRedirectMessage{BroadcastPath: "/real", TrackName: "trackname"},
		},
		"names another track and session": {
			allowed: follows,
			to:      Redirect{URI: "moqt://relay-b.example/", BroadcastPath: "/real", TrackName: "video"},
			want:    &message.SubscribeRedirectMessage{ConnectURI: "moqt://relay-b.example/", BroadcastPath: "/real", TrackName: "video"},
		},
		"peer does not follow redirects": {
			allowed: refuses,
			to:      Redirect{BroadcastPath: "/real"},
			wantErr: ErrRedirectNotSupported,
		},
		"writer outside a session": {
			to:      Redirect{BroadcastPath: "/real"},
			wantErr: ErrRedirectNotSupported,
		},
		"after SUBSCRIBE_OK": {
			allowed: follows,
			okSent:  true,
			to:      Redirect{BroadcastPath: "/real"},
			wantErr: errSubscriptionAnswered,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tw, buf := newTrackWriterDropTestSender(t)
			tw.redirectAllowed = tt.allowed
			if tt.okSent {
				require.NoError(t, tw.subscribeStream.ensureOk(1))
			}
			before := buf.Len()

			err := tw.Redirect(t.Context(), tt.to)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, before, buf.Len(), "a refused redirect writes nothing")
				return
			}
			require.NoError(t, err)
			require.NoError(t, tw.Close())

			b := buf.Bytes()
			require.NotEmpty(t, b)
			assert.Equal(t, byte(message.MessageTypeSubscribeRedirect), b[0])
			r := bytes.NewReader(b[1:])
			var got message.SubscribeRedirectMessage
			require.NoError(t, got.Decode(r))
			assert.Equal(t, *tt.want, got)
			assert.Zero(t, r.Len(), "nothing follows the redirect, not even SUBSCRIBE_END on Close")
		})
	}
}

func TestTrackWriter_Redirect_StopsTheWriter(t *testing.T) {
	tw, buf := newTrackWriterDropTestSender(t)
	tw.redirectAllowed = func(context.Context, string) error { return nil }
	require.NoError(t, tw.Redirect(t.Context(), Redirect{BroadcastPath: "/real"}))
	sent := buf.Len()

	_, err := tw.OpenGroup(t.Context())
	assert.Error(t, err, "no group opens after a redirect")
	assert.NotNil(t, tw.TrackConfig(), "still readable after a redirect")
	assert.NoError(t, tw.Close())
	tw.CloseWithError(SubscribeErrorCodeInternal)
	assert.Equal(t, sent, buf.Len(), "nothing is sent after a redirect")
	assert.ErrorIs(t, tw.Redirect(t.Context(), Redirect{BroadcastPath: "/real"}), errSubscriptionAnswered)
}

func TestTrackWriter_Redirect_StreamFailures(t *testing.T) {
	errWrite := errors.New("write failed")
	errClose := errors.New("close failed")

	tests := map[string]struct {
		stream    *FakeQUICStream
		wantErr   error
		wantReset bool
	}{
		"write fails: reset, and nothing follows": {
			stream:    &FakeQUICStream{Writes: []streamResult{{Err: errWrite}}},
			wantErr:   errWrite,
			wantReset: true,
		},
		"close fails after the redirect is out: still sent": {
			stream: &FakeQUICStream{CloseErr: errClose},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			substr := newReceiveSubscribeStream(SubscribeID(1), tt.stream, &SubscribeConfig{})
			tw := newTrackWriter("/alias", "video", substr, nil, func() {})
			tw.redirectAllowed = func(context.Context, string) error { return nil }

			err := tw.Redirect(t.Context(), Redirect{BroadcastPath: "/real"})
			_ = tw.Close() // not actionable: what Close sends is asserted below

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			if tt.wantReset {
				assert.Contains(t, tt.stream.CancelWriteCodes(), transport.StreamErrorCode(SubscribeErrorCodeInternal))
			}
			// The writer is stopped either way: no group opens after the
			// subscription was redirected or reset.
			_, openErr := tw.OpenGroup(t.Context())
			assert.Error(t, openErr)
			assert.Equal(t, 1, tt.stream.WriteCalls(), "the redirect goes out in one write, and SUBSCRIBE_END never follows")
		})
	}
}

func TestSession_redirectAllowed_WaitsForSetupUntilCtx(t *testing.T) {
	sess := newSession(&FakeStreamConn{}, NewTrackMux(0), nil, nil, nil, nil, nil, nil, sessionSetup{server: true}, nil)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := sess.redirectAllowed(ctx, "")

	assert.ErrorIs(t, err, context.Canceled)
}

func TestSendSubscribeStream_LateRedirect(t *testing.T) {
	tests := map[string]struct {
		first byte
		msg   interface{ Encode(io.Writer) error }
	}{
		"after SUBSCRIBE_OK":  {first: byte(message.MessageTypeSubscribeOk), msg: message.SubscribeOkMessage{Group: 1}},
		"after SUBSCRIBE_END": {first: byte(message.MessageTypeSubscribeEnd), msg: message.SubscribeEndMessage{Group: 1}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteByte(tt.first)
			require.NoError(t, tt.msg.Encode(&buf))
			buf.WriteByte(byte(message.MessageTypeSubscribeRedirect))
			require.NoError(t, message.SubscribeRedirectMessage{BroadcastPath: "/real", TrackName: "video"}.Encode(&buf))

			stream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}
			substr := newSendSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{})
			var violations []string
			substr.onProtocolViolation = func(msg string) { violations = append(violations, msg) }

			substr.readSubscribeResponses()

			assert.Len(t, violations, 1)
			_, closed := substr.closedSince()
			assert.True(t, closed, "the subscription ends")
		})
	}
}

func TestSession_Subscribe_MalformedRedirect(t *testing.T) {
	// SUBSCRIBE_REDIRECT whose length says 3 bytes but whose first string
	// claims 5.
	stream := &FakeQUICStream{Reads: []streamResult{{Data: []byte{byte(message.MessageTypeSubscribeRedirect), 0x03, 0x05, 0x00, 0x00}}}}
	conn := &FakeStreamConn{OpenStreams: []biStreamResult{{Stream: stream}}}
	sess := newTestSession(conn)
	t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

	tr, err := sess.Subscribe(t.Context(), "/alias", "video", nil)

	assert.Error(t, err)
	assert.Nil(t, tr)
	assert.Contains(t, stream.CancelWriteCodes(), transport.StreamErrorCode(SubscribeErrorCodeInternal))
}

func TestSession_followRedirect_URIToAServer(t *testing.T) {
	sess, conn := newTestSessionWithConn(t)
	sess.server = true

	tr, err := sess.followRedirect(t.Context(), Redirect{URI: "moqt://b/", BroadcastPath: "/real", TrackName: "video"}, &SubscribeConfig{}, nil)

	assert.ErrorIs(t, err, ErrClientRedirectURI)
	assert.Nil(t, tr)
	select {
	case <-conn.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("the session was not closed with PROTOCOL_VIOLATION")
	}
}

func TestSession_redirectAllowed_PeerSetup(t *testing.T) {
	tests := map[string]struct {
		setupArrives bool
		peerFollows  bool
		wantErr      error
	}{
		"arrives late, follows":        {setupArrives: true, peerFollows: true},
		"arrives late, does not":       {setupArrives: true, wantErr: ErrRedirectNotSupported},
		"never arrives: setup timeout": {wantErr: ErrRedirectNotSupported},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sess := newSession(&FakeStreamConn{}, NewTrackMux(0), nil, &Config{SetupTimeout: 200 * time.Millisecond},
				nil, nil, nil, nil, sessionSetup{server: true}, nil)
			t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })

			if tt.setupArrives {
				go func() {
					time.Sleep(20 * time.Millisecond) // after redirectAllowed starts waiting
					sess.peerFollowsRedirect = tt.peerFollows
					markPeerSetupReceived(sess, message.ProbeLevelNone)
				}()
			}

			err := sess.redirectAllowed(t.Context(), "")

			switch {
			case tt.wantErr != nil:
				assert.ErrorIs(t, err, tt.wantErr)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

func TestSession_OpenSetupStream_AdvertisesRedirect(t *testing.T) {
	conn, wait := collectSetupStream(t)
	sess := newSession(conn, NewTrackMux(0), nil, nil, nil, nil, nil, nil, sessionSetup{}, nil)
	defer sess.CloseWithError(NoError, "") //nolint:errcheck // not actionable: cleanup

	sm := decodeSetupBytes(t, wait())

	assert.True(t, sm.FollowsRedirect(), "every session follows redirects")
}

func TestSession_PeerFollowsRedirect(t *testing.T) {
	setup := func(follows bool) message.SetupMessage {
		var sm message.SetupMessage
		if follows {
			sm.AddRedirect()
		}
		return sm
	}

	for _, follows := range []bool{true, false} {
		t.Run(fmt.Sprintf("Setup Stream, follows=%t", follows), func(t *testing.T) {
			sess := newSession(&FakeStreamConn{}, NewTrackMux(0), nil, nil, nil, nil, nil, nil, sessionSetup{}, nil)
			defer sess.CloseWithError(NoError, "") //nolint:errcheck // not actionable: cleanup
			var buf bytes.Buffer
			require.NoError(t, setup(follows).Encode(&buf))

			sess.handleSetupStream(&FakeQUICReceiveStream{Reads: []streamResult{{Data: buf.Bytes()}}})

			<-sess.peerSetupCh
			assert.Equal(t, follows, sess.peerFollowsRedirect)
		})
		t.Run(fmt.Sprintf("native QUIC router, follows=%t", follows), func(t *testing.T) {
			sm := setup(follows)
			sess := newSession(&FakeStreamConn{}, NewTrackMux(0), nil, nil, nil, nil, nil, nil,
				sessionSetup{server: true, peerSetup: &sm}, nil)
			defer sess.CloseWithError(NoError, "") //nolint:errcheck // not actionable: cleanup

			assert.Equal(t, follows, sess.peerFollowsRedirect)
		})
	}
}

func TestTrackWriter_Redirect_Invalid(t *testing.T) {
	tests := map[string]Redirect{
		"empty path":    {},
		"relative path": {BroadcastPath: "real"},
	}
	for name, to := range tests {
		t.Run(name, func(t *testing.T) {
			tw, buf := newTrackWriterDropTestSender(t)
			tw.redirectAllowed = func(context.Context, string) error { return nil }

			assert.Error(t, tw.Redirect(t.Context(), to))
			assert.Zero(t, buf.Len())
		})
	}
}

func TestSession_redirectAllowed(t *testing.T) {
	tests := map[string]struct {
		server      bool
		peerFollows bool
		uri         string
		wantErr     error
	}{
		"server, same session":           {server: true, peerFollows: true},
		"server, another session":        {server: true, peerFollows: true, uri: "moqt://b/"},
		"client, same session":           {peerFollows: true},
		"client, another session":        {peerFollows: true, uri: "moqt://b/", wantErr: ErrClientRedirectURI},
		"peer does not follow":           {server: true, wantErr: ErrRedirectNotSupported},
		"client URI checked before peer": {uri: "moqt://b/", wantErr: ErrClientRedirectURI},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sess, _ := newTestSessionWithConn(t)
			sess.server = tt.server
			sess.peerFollowsRedirect = tt.peerFollows

			err := sess.redirectAllowed(t.Context(), tt.uri)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSession_allowRedirect(t *testing.T) {
	to := Redirect{BroadcastPath: "/real", TrackName: "video"}
	errVeto := errors.New("vetoed")
	allowAll := func(Redirect, []Redirect) error { return nil }

	tests := map[string]struct {
		check   CheckRedirectFunc
		via     int
		wantErr error
	}{
		"default, first":           {via: 0},
		"default, second":          {via: 1},
		"default, third":           {via: 2, wantErr: ErrTooManyRedirects},
		"hook allows past default": {check: allowAll, via: 5},
		"hook allows, hard limit":  {check: allowAll, via: maxRedirects, wantErr: ErrTooManyRedirects},
		"hook vetoes first":        {check: func(Redirect, []Redirect) error { return errVeto }, wantErr: errVeto},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sess, _ := newTestSessionWithConn(t)
			sess.checkRedirect = tt.check

			err := sess.allowRedirect(to, make([]Redirect, tt.via))

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSession_followRedirect_InvalidPath(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)

	tr, err := sess.followRedirect(t.Context(), Redirect{BroadcastPath: "live/x", TrackName: "video"}, &SubscribeConfig{}, nil)

	assert.Nil(t, tr)
	redirect, ok := errors.AsType[*RedirectError](err)
	require.True(t, ok, "want a *RedirectError, got %v", err)
	assert.Equal(t, BroadcastPath("live/x"), redirect.Redirect.BroadcastPath)
}
