package moqt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSession_CloseWithError_AfterGoaway verifies a GOAWAY from the peer only
// drains the session: the session refuses new work, but CloseWithError still
// closes the connection, which the server's deferred cleanup relies on.
func TestSession_CloseWithError_AfterGoaway(t *testing.T) {
	session, conn := newTestSessionWithConn(t)

	var goaway bytes.Buffer
	require.NoError(t, message.GoawayMessage{NewSessionURI: "https://example.com/next"}.Encode(&goaway))
	stream := &FakeQUICStream{Reads: []streamResult{{Data: goaway.Bytes()}, {Err: io.EOF}}}

	require.NoError(t, session.handleGoawayStream(stream))
	assert.True(t, session.terminating(), "a draining session refuses new work")

	require.NoError(t, session.CloseWithError(NoError, "done"))

	assert.Equal(t, []closeCall{{Code: transport.ConnErrorCode(NoError), Reason: "done"}}, conn.CloseCalls())
}

// TestSession_CloseWithError_ConcurrentClosesOnce verifies concurrent callers
// close the connection exactly once.
func TestSession_CloseWithError_ConcurrentClosesOnce(t *testing.T) {
	session, conn := newTestSessionWithConn(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = session.CloseWithError(NoError, "") })
	}
	wg.Wait()

	assert.Len(t, conn.CloseCalls(), 1)
}

// TestSession_Subscribe_FailureRemovesTrackReader verifies a subscription that
// fails after it was registered is unregistered, so failed subscriptions do
// not accumulate in the session.
func TestSession_Subscribe_FailureRemovesTrackReader(t *testing.T) {
	var drop bytes.Buffer
	require.NoError(t, message.SubscribeDropMessage{GroupStart: 1, GroupEnd: 2}.Encode(&drop))

	tests := map[string]streamResult{
		"SUBSCRIBE_DROP first": {Data: append([]byte{byte(message.MessageTypeSubscribeDrop)}, drop.Bytes()...)},
		"stream reset":         {Err: &transport.StreamError{ErrorCode: transport.StreamErrorCode(SubscribeErrorCodeInternal), Remote: true}},
		"read error":           {Err: errors.New("read error")},
	}
	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			session, conn := newTestSessionWithConn(t)
			conn.OpenStreams = []biStreamResult{{Stream: &FakeQUICStream{Reads: []streamResult{response}}}}

			reader, err := session.Subscribe(context.Background(), "/test", "video", nil)

			require.Error(t, err)
			assert.Nil(t, reader)
			session.trackReaderMapLocker.RLock()
			defer session.trackReaderMapLocker.RUnlock()
			assert.Empty(t, session.trackReaders)
		})
	}
}

// TestAnnouncementReader_PeerEndsStream verifies that when the peer ends the
// announce stream, a blocked ReceiveAnnouncement returns and the
// announcements still active end.
func TestAnnouncementReader_PeerEndsStream(t *testing.T) {
	tests := map[string]streamResult{
		"FIN":   {Err: io.EOF},
		"reset": {Err: &transport.StreamError{ErrorCode: transport.StreamErrorCode(AnnounceErrorCodeInternal), Remote: true}},
	}
	for name, end := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, message.AnnounceOkMessage{}.Encode(&buf))
			require.NoError(t, message.AnnounceBroadcastMessage{BroadcastPathSuffix: "stream1", AnnounceStatus: message.ACTIVE}.Encode(&buf))
			stream := &FakeQUICStream{Reads: []streamResult{{Data: buf.Bytes()}, end}}

			ar := newAnnouncementReader(stream, "/test/", nil)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ann, err := ar.ReceiveAnnouncement(ctx)
			require.NoError(t, err)

			_, err = ar.ReceiveAnnouncement(ctx)
			require.Error(t, err)
			assert.NoError(t, ctx.Err(), "ReceiveAnnouncement must return because the stream ended, not on the test timeout")
			select {
			case <-ann.Done():
			case <-ctx.Done():
				t.Fatal("the active announcement did not end with the stream")
			}
		})
	}
}

// TestSession_Fetch_ContextLifecycle verifies the request's context cancels a
// fetch still in progress, and that once the group has ended the session stops
// watching that context, so canceling it later touches nothing.
func TestSession_Fetch_ContextLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cases := []struct {
			name       string
			readFirst  bool
			wantCancel []transport.StreamErrorCode
		}{
			{name: "context ends first", wantCancel: []transport.StreamErrorCode{transport.StreamErrorCode(ExpiredGroupErrorCode)}},
			{name: "group ends first", readFirst: true, wantCancel: []transport.StreamErrorCode{}},
		}
		// t.Run is unsupported inside a synctest bubble, so the cases run in
		// a plain loop.
		for _, tc := range cases {
			stream := &FakeQUICStream{Reads: []streamResult{{Err: io.EOF}}}
			session := newTestSession(&FakeStreamConn{OpenStreams: []biStreamResult{{Stream: stream}}})
			ctx, cancel := context.WithCancel(context.Background())

			group, err := session.Fetch((&FetchRequest{BroadcastPath: "/test", TrackName: "video", GroupSequence: 1}).WithContext(ctx))
			require.NoError(t, err, tc.name)
			if tc.readFirst {
				require.ErrorIs(t, group.ReadFrame(NewFrame(0)), io.EOF, tc.name)
			}
			cancel()
			// Let a still-registered AfterFunc callback run.
			synctest.Wait()

			assert.Equal(t, tc.wantCancel, stream.CancelReadCodes(), tc.name)
			require.NoError(t, session.CloseWithError(NoError, ""), tc.name)
		}
	})
}
