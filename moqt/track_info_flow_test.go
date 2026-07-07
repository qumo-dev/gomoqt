package moqt

import (
	"bytes"
	"context"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_TrackInfo(t *testing.T) {
	tests := map[string]struct {
		response message.TrackInfoMessage
		wantErr  bool
		expected *PublishInfo
	}{
		"valid track info": {
			response: message.TrackInfoMessage{
				PublisherPriority:   5,
				PublisherOrdered:    1,
				PublisherMaxLatency: 2000,
				Timescale:           90000,
			},
			expected: &PublishInfo{
				Priority:   5,
				Ordered:    true,
				MaxLatency: 2000,
				Timescale:  90000,
			},
		},
		"zero timescale is a protocol violation": {
			response: message.TrackInfoMessage{Timescale: 0},
			wantErr:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var written bytes.Buffer
			var response bytes.Buffer
			require.NoError(t, tt.response.Encode(&response))

			stream := &FakeQUICStream{
				WriteFunc: written.Write,
				ReadFunc:  response.Read,
			}
			conn := &FakeStreamConn{
				OpenStreamFunc: func() (transport.Stream, error) { return stream, nil },
			}
			sess := newTestSession(conn)
			defer sess.CloseWithError(NoError, "")

			info, err := sess.TrackInfo(context.Background(), "/live/alice", "video")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info)

			// Verify the request wire bytes: stream type then TRACK message.
			r := bytes.NewReader(written.Bytes())
			var st message.StreamType
			require.NoError(t, st.Decode(r))
			assert.Equal(t, message.StreamTypeTrack, st)
			var tm message.TrackMessage
			require.NoError(t, tm.Decode(r))
			assert.Equal(t, "/live/alice", tm.BroadcastPath)
			assert.Equal(t, "video", tm.TrackName)
		})
	}
}

func TestSession_HandleTrackStream(t *testing.T) {
	tests := map[string]struct {
		register  func(mux *TrackMux)
		path      string
		track     string
		wantReset bool
		expected  message.TrackInfoMessage
	}{
		"registered track with explicit info": {
			register: func(mux *TrackMux) {
				b := NewBroadcast()
				require.NoError(t, b.RegisterWithInfo("video", PublishInfo{
					Priority:   7,
					Ordered:    true,
					MaxLatency: 500,
					Timescale:  48000,
				}, TrackHandlerFunc(func(tw *TrackWriter) {})))
				mux.Publish(context.Background(), "/live/alice", b)
			},
			path:  "/live/alice",
			track: "video",
			expected: message.TrackInfoMessage{
				PublisherPriority:   7,
				PublisherOrdered:    1,
				PublisherMaxLatency: 500,
				Timescale:           48000,
			},
		},
		"registered track without info gets defaults": {
			register: func(mux *TrackMux) {
				mux.PublishFunc(context.Background(), "/live/alice", func(tw *TrackWriter) {})
			},
			path:  "/live/alice",
			track: "video",
			expected: message.TrackInfoMessage{
				Timescale: DefaultTimescale,
			},
		},
		"unknown broadcast resets the stream": {
			register:  func(mux *TrackMux) {},
			path:      "/missing",
			track:     "video",
			wantReset: true,
		},
		"unknown track name in broadcast resets the stream": {
			register: func(mux *TrackMux) {
				b := NewBroadcast()
				require.NoError(t, b.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {})))
				mux.Publish(context.Background(), "/live/alice", b)
			},
			path:      "/live/alice",
			track:     "audio",
			wantReset: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mux := NewTrackMux(0)
			tt.register(mux)

			var request bytes.Buffer
			require.NoError(t, message.TrackMessage{
				BroadcastPath: tt.path,
				TrackName:     tt.track,
			}.Encode(&request))

			var written bytes.Buffer
			var reset bool
			stream := &FakeQUICStream{
				ReadFunc:  request.Read,
				WriteFunc: written.Write,
				CancelWriteFunc: func(transport.StreamErrorCode) {
					reset = true
				},
			}

			conn := &FakeStreamConn{}
			sess := newSession(conn, mux, nil, nil, nil, nil, nil, sessionSetup{})
			defer sess.CloseWithError(NoError, "")

			sess.handleTrackStream(stream)

			if tt.wantReset {
				assert.True(t, reset, "stream should be reset for an unknown track")
				return
			}

			var tim message.TrackInfoMessage
			require.NoError(t, tim.Decode(&written))
			assert.Equal(t, tt.expected, tim)
		})
	}
}
