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

// TestSession_handleGoawayStream verifies a GOAWAY from the peer is a hint:
// it reaches onGoaway and leaves the session open, and CloseWithError still
// closes the connection, which the server's deferred cleanup relies on.
func TestSession_handleGoawayStream(t *testing.T) {
	session, conn := newTestSessionWithConn(t)
	var gotURI string
	session.onGoaway = func(uri string) { gotURI = uri }

	var goaway bytes.Buffer
	require.NoError(t, message.GoawayMessage{NewSessionURI: "https://example.com/next"}.Encode(&goaway))
	stream := &FakeQUICStream{Reads: []streamResult{{Data: goaway.Bytes()}, {Err: io.EOF}}}

	require.NoError(t, session.handleGoawayStream(stream))

	assert.Equal(t, "https://example.com/next", gotURI)
	assert.False(t, session.closed.Load(), "GOAWAY leaves the session open")
	conn.OpenStreams = []biStreamResult{{Stream: &FakeQUICStream{}}}
	_, err := session.Fetch(&FetchRequest{BroadcastPath: "/test", TrackName: "video", GroupSequence: 1})
	require.NoError(t, err, "the session still serves new work after GOAWAY")
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

// TestSession_Subscribe_FailureCancelsQueuedGroup verifies a group stream that
// arrived before the subscription failed is cancelled, not left queued in a
// reader no one holds.
func TestSession_Subscribe_FailureCancelsQueuedGroup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		// The response read parks until gate closes, then fails.
		subStream := &FakeQUICStream{Reads: []streamResult{{Block: true}, {Err: errors.New("read error")}}, ReadGate: gate}
		session := newTestSession(&FakeStreamConn{OpenStreams: []biStreamResult{{Stream: subStream}}})
		defer func() { assert.NoError(t, session.CloseWithError(NoError, "")) }()

		errCh := make(chan error, 1)
		go func() {
			_, err := session.Subscribe(context.Background(), "/test", "video", nil)
			errCh <- err
		}()
		// Subscribe is registered and parked on the response.
		synctest.Wait()

		var group bytes.Buffer
		require.NoError(t, message.StreamTypeGroup.Encode(&group))
		require.NoError(t, message.GroupMessage{SubscribeID: 1, GroupSequence: 1}.Encode(&group))
		groupStream := &FakeQUICReceiveStream{Reads: []streamResult{{Data: group.Bytes()}, {Block: true}}}
		session.processUniStream(groupStream)

		close(gate)
		require.Error(t, <-errCh)

		assert.Equal(t, []transport.StreamErrorCode{transport.StreamErrorCode(SubscribeErrorCodeInternal)}, groupStream.CancelReadCodes())
	})
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
// fetch still in progress, and that once the group has ended for the reader
// (it read to the end, or cancelled) the session stops watching that context,
// so canceling it later touches nothing.
func TestSession_Fetch_ContextLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		expired := transport.StreamErrorCode(ExpiredGroupErrorCode)
		canceled := transport.StreamErrorCode(SubscribeCanceledErrorCode)
		cases := []struct {
			name string
			// endGroup, when set, ends the group before the context is canceled.
			endGroup   func(*testing.T, *GroupReader)
			wantCancel []transport.StreamErrorCode
		}{
			{name: "context ends first", wantCancel: []transport.StreamErrorCode{expired}},
			{
				name: "group read to the end first",
				endGroup: func(t *testing.T, group *GroupReader) {
					require.ErrorIs(t, group.ReadFrame(NewFrame(0)), io.EOF)
				},
				wantCancel: []transport.StreamErrorCode{},
			},
			{
				name:       "group cancelled first",
				endGroup:   func(_ *testing.T, group *GroupReader) { group.CancelRead(SubscribeCanceledErrorCode) },
				wantCancel: []transport.StreamErrorCode{canceled},
			},
		}
		// t.Run is unsupported inside a synctest bubble, so the cases run in
		// a plain loop.
		for _, tc := range cases {
			stream := &FakeQUICStream{Reads: []streamResult{{Err: io.EOF}}}
			session := newTestSession(&FakeStreamConn{OpenStreams: []biStreamResult{{Stream: stream}}})
			ctx, cancel := context.WithCancel(context.Background())

			group, err := session.Fetch((&FetchRequest{BroadcastPath: "/test", TrackName: "video", GroupSequence: 1}).WithContext(ctx))
			require.NoError(t, err, tc.name)
			if tc.endGroup != nil {
				tc.endGroup(t, group)
			}
			cancel()
			// Let a still-registered AfterFunc callback run.
			synctest.Wait()

			assert.Equal(t, tc.wantCancel, stream.CancelReadCodes(), tc.name)
			require.NoError(t, session.CloseWithError(NoError, ""), tc.name)
		}
	})
}
