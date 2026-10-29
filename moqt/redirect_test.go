package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
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

func TestSendSubscribeStream_RedirectAfterOk(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(byte(message.MessageTypeSubscribeOk))
	require.NoError(t, message.SubscribeOkMessage{Group: 1}.Encode(&buf))
	buf.WriteByte(byte(message.MessageTypeSubscribeRedirect))
	require.NoError(t, message.SubscribeRedirectMessage{BroadcastPath: "/real", TrackName: "video"}.Encode(&buf))

	stream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, {Err: io.EOF}}}
	substr := newSendSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{})
	var violations []string
	substr.onProtocolViolation = func(msg string) { violations = append(violations, msg) }

	substr.readSubscribeResponses()

	assert.Len(t, violations, 1)
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

// TestSession_Subscribe_Redirect runs a real server whose mux redirects some
// broadcasts, and checks what a dialed client's Subscribe returns for each.
func TestSession_Subscribe_Redirect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	// redirectTo serves every subscription with a redirect to to, as an
	// application's handler on a TrackMux does.
	redirectTo := func(to Redirect) func(*TrackWriter) {
		return func(tw *TrackWriter) {
			if err := tw.Redirect(tw.Context(), to); err != nil {
				tw.CloseWithError(SubscribeErrorCodeNotFound)
			}
		}
	}
	elsewhere := Redirect{URI: "moqt://relay-b.example/", BroadcastPath: "/real", TrackName: "video"}

	mux := NewTrackMux(0)
	mux.PublishFunc(ctx, "/real", func(tw *TrackWriter) {
		gw, err := tw.OpenGroup(ctx)
		if err != nil {
			return
		}
		frame := NewFrame(8)
		frame.Write([]byte(tw.TrackName))
		_ = gw.WriteFrame(frame) // not actionable: the subscriber reports what it read
		_ = gw.Close()           // not actionable: as above
		<-tw.Context().Done()
	})
	mux.PublishFunc(ctx, "/alias", redirectTo(Redirect{BroadcastPath: "/real"}))
	mux.PublishFunc(ctx, "/alias-of-alias", redirectTo(Redirect{BroadcastPath: "/alias"}))
	mux.PublishFunc(ctx, "/loop-a", redirectTo(Redirect{BroadcastPath: "/loop-b"}))
	mux.PublishFunc(ctx, "/loop-b", redirectTo(Redirect{BroadcastPath: "/loop-a"}))
	mux.PublishFunc(ctx, "/elsewhere", redirectTo(elsewhere))

	addr := freePort(t)
	server := &Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			NextProtos:   []string{NextProtoH3, NextProtoMOQ},
			Certificates: []tls.Certificate{generateTestCert(t)},
		},
		QUICConfig: &quic.Config{EnableDatagrams: true},
		TrackMux:   mux,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler:    HandleFunc(func(sess *Session) { <-sess.Context().Done() }),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ErrServerClosed) {
			t.Logf("server: %v", err)
		}
	}()
	t.Cleanup(func() { closeServer(t, server) })

	dial := func(config *Config) *Session {
		client := Dialer{
			Config:    config,
			Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
		}
		var sess *Session
		require.Eventually(t, func() bool {
			s, err := client.Dial(ctx, "moqt://"+addr+"/", nil)
			if err != nil {
				return false
			}
			sess = s
			return true
		}, 5*time.Second, 50*time.Millisecond, "dial the test server")
		t.Cleanup(func() { _ = sess.CloseWithError(NoError, "") })
		return sess
	}

	// hooked follows a redirect to /real only as the first of a subscription,
	// and stops any to /loop-b.
	errVeto := errors.New("vetoed")
	hooked := dial(&Config{CheckRedirect: func(to Redirect, via []Redirect) error {
		switch {
		case to.BroadcastPath == "/loop-b":
			return ErrStopRedirect
		case to.BroadcastPath == "/real" && len(via) > 0:
			return errVeto
		}
		return nil
	}})
	plain := dial(nil)

	tests := map[string]struct {
		sess     *Session
		path     BroadcastPath
		wantPath BroadcastPath
		wantErr  error
		wantTo   *Redirect
	}{
		"no redirect":     {sess: plain, path: "/real", wantPath: "/real"},
		"one redirect":    {sess: plain, path: "/alias", wantPath: "/real"},
		"two redirects":   {sess: plain, path: "/alias-of-alias", wantPath: "/real"},
		"loop":            {sess: plain, path: "/loop-a", wantErr: ErrTooManyRedirects, wantTo: &Redirect{BroadcastPath: "/loop-b", TrackName: "video"}},
		"another session": {sess: plain, path: "/elsewhere", wantTo: &elsewhere},
		"hook follows":    {sess: hooked, path: "/alias", wantPath: "/real"},
		"hook vetoes":     {sess: hooked, path: "/alias-of-alias", wantErr: errVeto, wantTo: &Redirect{BroadcastPath: "/real", TrackName: "video"}},
		"hook stops":      {sess: hooked, path: "/loop-a", wantTo: &Redirect{BroadcastPath: "/loop-b", TrackName: "video"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tr, err := tt.sess.Subscribe(ctx, tt.path, "video", nil)

			if tt.wantTo != nil {
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				assert.NotErrorIs(t, err, ErrStopRedirect, "a stopped redirect is returned as itself")
				redirect, ok := errors.AsType[*RedirectError](err)
				require.True(t, ok, "want a *RedirectError, got %v", err)
				assert.Equal(t, *tt.wantTo, redirect.Redirect)
				return
			}
			require.NoError(t, err)
			defer tr.Close()
			assert.Equal(t, tt.wantPath, tr.BroadcastPath)
			assert.Equal(t, TrackName("video"), tr.TrackName)

			gr, err := tr.AcceptGroup(ctx)
			require.NoError(t, err)
			frame := NewFrame(8)
			require.NoError(t, gr.ReadFrame(frame))
			assert.Equal(t, []byte("video"), frame.Body())
		})
	}
}

func TestConfig_checkRedirect(t *testing.T) {
	to := Redirect{BroadcastPath: "/real", TrackName: "video"}
	errVeto := errors.New("vetoed")

	tests := map[string]struct {
		config  *Config
		via     int
		wantErr error
	}{
		"nil config, first":      {config: nil, via: 0},
		"default, second":        {config: &Config{}, via: 1},
		"default, third":         {config: &Config{}, via: 2, wantErr: ErrTooManyRedirects},
		"hook allows past limit": {config: &Config{CheckRedirect: func(Redirect, []Redirect) error { return nil }}, via: 5},
		"hook vetoes first":      {config: &Config{CheckRedirect: func(Redirect, []Redirect) error { return errVeto }}, via: 0, wantErr: errVeto},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.config.checkRedirect(to, make([]Redirect, tt.via))
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestConfig_Clone_CheckRedirect(t *testing.T) {
	errVeto := errors.New("vetoed")
	original := &Config{CheckRedirect: func(Redirect, []Redirect) error { return errVeto }}

	cloned := original.Clone()

	require.NotNil(t, cloned.CheckRedirect)
	assert.ErrorIs(t, cloned.CheckRedirect(Redirect{}, nil), errVeto)
}
