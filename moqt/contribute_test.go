package moqt

import (
	"bytes"
	"context"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contributeRequest is the CONTRIBUTE_REQUEST a peer sends after the stream type.
func contributeRequest(t *testing.T, path, track string) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, message.ContributeRequestMessage{BroadcastPath: path, TrackName: track}.Encode(&buf))
	return buf.Bytes()
}

// subscribeRequest is the SUBSCRIBE a receiver sends on a Contribute Stream.
func subscribeRequest(t *testing.T, id uint64, path, track string) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, message.SubscribeMessage{SubscribeID: id, BroadcastPath: path, TrackName: track}.Encode(&buf))
	return buf.Bytes()
}

func subscribeOkResponse(t *testing.T, group uint64) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteByte(byte(message.MessageTypeSubscribeOk))
	require.NoError(t, message.SubscribeOkMessage{Group: group}.Encode(&buf))
	return buf.Bytes()
}

// contributeTrackHandler is a broadcast's handler that also takes
// contributions: the optional interface under test.
type contributeTrackHandler struct {
	ContributeHandlerFunc
}

func (contributeTrackHandler) ServeTrack(*TrackWriter) {}

// acceptContributions registers a handler for path that serves contributions
// with serve.
func acceptContributions(mux *TrackMux, path BroadcastPath, serve ContributeHandlerFunc) {
	mux.Publish(context.Background(), path, contributeTrackHandler{serve})
}

func TestSession_Contribute_ReturnsAWriterOnceThePeerSubscribes(t *testing.T) {
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: subscribeRequest(t, 94, "/room/1/chat", "alice")}},
	}
	sess, _ := newTestSessionWithConn(t, func(conn *FakeStreamConn) {
		conn.OpenStreams = []biStreamResult{{Stream: stream}}
	})

	tw, err := sess.Contribute(context.Background(), "/room/1/chat", "alice", nil)

	require.NoError(t, err)
	require.NotNil(t, tw)
	defer tw.Close()
	assert.Equal(t, BroadcastPath("/room/1/chat"), tw.BroadcastPath)
	assert.Equal(t, TrackName("alice"), tw.TrackName)

	// The stream opens with the Contribute type and CONTRIBUTE_REQUEST.
	written := bytes.NewReader(stream.Written())
	var streamType message.StreamType
	require.NoError(t, streamType.Decode(written))
	assert.Equal(t, message.StreamTypeContribute, streamType)
	var crm message.ContributeRequestMessage
	require.NoError(t, crm.Decode(written))
	assert.Equal(t, message.ContributeRequestMessage{BroadcastPath: "/room/1/chat", TrackName: "alice"}, crm)
	assert.Zero(t, written.Len(), "nothing follows until the writer is used")
}

func TestSession_Contribute_RejectsASubscribeForAnotherTrack(t *testing.T) {
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: subscribeRequest(t, 94, "/room/1/chat", "bob")}},
	}
	sess, _ := newTestSessionWithConn(t, func(conn *FakeStreamConn) {
		conn.OpenStreams = []biStreamResult{{Stream: stream}}
	})

	tw, err := sess.Contribute(context.Background(), "/room/1/chat", "alice", nil)

	require.Error(t, err)
	assert.Nil(t, tw)
	assert.NotEmpty(t, stream.CancelWriteCodes(), "the stream is reset")
}

func TestSession_Contribute_InvalidPath(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)

	tw, err := sess.Contribute(context.Background(), "room/1/chat", "alice", nil)

	assert.Error(t, err)
	assert.Nil(t, tw)
}

// TestSession_Contribute_AnswersTrackWhileTheWriterIsOpen checks TRACK is
// answered for a contributed track, although the endpoint announces no such
// path, and only while it is being contributed.
func TestSession_Contribute_AnswersTrackWhileTheWriterIsOpen(t *testing.T) {
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: subscribeRequest(t, 94, "/room/1/chat", "alice")}},
	}
	sess, _ := newTestSessionWithConn(t, func(conn *FakeStreamConn) {
		conn.OpenStreams = []biStreamResult{{Stream: stream}}
	})
	tw, err := sess.Contribute(context.Background(), "/room/1/chat", "alice", &PublishInfo{Timescale: 48000})
	require.NoError(t, err)
	var request bytes.Buffer
	require.NoError(t, message.TrackMessage{BroadcastPath: "/room/1/chat", TrackName: "alice"}.Encode(&request))

	open := &FakeQUICStream{Reads: []streamResult{{Data: request.Bytes()}}}
	sess.handleTrackStream(open)

	var info message.TrackInfoMessage
	require.NoError(t, info.Decode(bytes.NewReader(open.Written())))
	assert.Equal(t, uint64(48000), info.Timescale)

	require.NoError(t, tw.Close())
	closed := &FakeQUICStream{Reads: []streamResult{{Data: request.Bytes()}}}
	sess.handleTrackStream(closed)

	assert.NotEmpty(t, closed.CancelWriteCodes(), "the path is unknown again")
}

func TestSession_HandleContributeStream_Refused(t *testing.T) {
	tests := map[string]func(mux *TrackMux){
		"no handler for the path": func(*TrackMux) {},
		"a handler that does not take contributions": func(mux *TrackMux) {
			mux.PublishFunc(context.Background(), "/room/1/chat", func(*TrackWriter) {})
		},
		"a handler for a broader path": func(mux *TrackMux) {
			acceptContributions(mux, "/room/1", func(*ContributeResponseWriter, *ContributeRequest) {})
		},
	}
	for name, register := range tests {
		t.Run(name, func(t *testing.T) {
			sess, _ := newTestSessionWithConn(t)
			register(sess.mux)
			stream := &FakeQUICStream{
				Reads: []streamResult{{Data: contributeRequest(t, "/room/1/chat", "alice")}},
			}

			sess.handleContributeStream(stream)

			assert.NotEmpty(t, stream.CancelWriteCodes())
			assert.Empty(t, stream.Written(), "nothing is sent to the publisher")
		})
	}
}

func TestSession_HandleContributeStream_ServesTheRequest(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)
	var got *ContributeRequest
	acceptContributions(sess.mux, "/room/1/chat", func(_ *ContributeResponseWriter, r *ContributeRequest) {
		got = r
	})
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: contributeRequest(t, "/room/1/chat", "alice")}},
	}

	sess.handleContributeStream(stream)

	require.NotNil(t, got)
	assert.Equal(t, BroadcastPath("/room/1/chat"), got.BroadcastPath)
	assert.Equal(t, TrackName("alice"), got.TrackName)
	assert.Empty(t, stream.Written(), "a contribution is not subscribed until the handler asks")
}

func TestContributeResponseWriter_Subscribe_UsesTheContributeStream(t *testing.T) {
	sess, conn := newTestSessionWithConn(t)
	stream := &FakeQUICStream{
		Reads: []streamResult{
			{Data: contributeRequest(t, "/room/1/chat", "alice")},
			{Data: subscribeOkResponse(t, 1)},
		},
	}
	opened := conn.OpenCalls()
	var subscribed SubscribeID
	var subscribeErr, secondErr error
	acceptContributions(sess.mux, "/room/1/chat", func(w *ContributeResponseWriter, r *ContributeRequest) {
		var reader *TrackReader
		reader, subscribeErr = w.Subscribe(r.Context(), nil)
		if reader != nil {
			subscribed = reader.SubscribeID()
		}
		_, secondErr = w.Subscribe(r.Context(), nil)
	})

	sess.handleContributeStream(stream)

	require.NoError(t, subscribeErr)
	assert.ErrorIs(t, secondErr, ErrContributeSubscribed)
	assert.Equal(t, opened, conn.OpenCalls(), "the subscription rides the contribute stream, not a new one")

	var sm message.SubscribeMessage
	require.NoError(t, sm.Decode(bytes.NewReader(stream.Written())))
	assert.Equal(t, "/room/1/chat", sm.BroadcastPath)
	assert.Equal(t, "alice", sm.TrackName)
	assert.Equal(t, uint64(subscribed), sm.SubscribeID)
}

func TestContributeResponseWriter_CloseWithError_ResetsTheStream(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)
	acceptContributions(sess.mux, "/room/1/chat", func(w *ContributeResponseWriter, _ *ContributeRequest) {
		w.CloseWithError(SubscribeErrorCodeUnauthorized)
	})
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: contributeRequest(t, "/room/1/chat", "alice")}},
	}

	sess.handleContributeStream(stream)

	assert.NotEmpty(t, stream.CancelWriteCodes())
}

func TestSession_HandleContributeStream_HandlerPanicResetsTheStream(t *testing.T) {
	sess, _ := newTestSessionWithConn(t)
	acceptContributions(sess.mux, "/room/1/chat", func(*ContributeResponseWriter, *ContributeRequest) {
		panic("boom")
	})
	stream := &FakeQUICStream{
		Reads: []streamResult{{Data: contributeRequest(t, "/room/1/chat", "alice")}},
	}

	sess.handleContributeStream(stream)

	assert.NotEmpty(t, stream.CancelWriteCodes())
}
