package moqt

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastRegisterAndServeTrack(t *testing.T) {
	broadcast := NewBroadcast()

	received := make(chan *TrackWriter, 1)
	err := broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		received <- tw
	}))
	require.NoError(t, err)

	tw := &TrackWriter{TrackName: "video"}
	broadcast.ServeTrack(tw)

	select {
	case got := <-received:
		assert.Same(t, tw, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected registered handler to receive track writer")
	}
}


func TestBroadcastRemoveClosesActiveTracks(t *testing.T) {
	broadcast := NewBroadcast()

	started := make(chan struct{})
	closed := make(chan struct{})
	done := make(chan struct{})

	err := broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		close(started)
		<-closed
	}))
	require.NoError(t, err)

	tw := &TrackWriter{
		TrackName:    "video",
		groupManager: newGroupWriterManager(),
		onCloseTrackFunc: func() {
			close(closed)
		},
	}

	go func() {
		broadcast.ServeTrack(tw)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected handler to start serving track")
	}

	assert.True(t, broadcast.Remove("video"))

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected active track to stop after handler removal")
	}
}

func TestBroadcastRegisterReplacementClosesPreviousActiveTracks(t *testing.T) {
	broadcast := NewBroadcast()

	started := make(chan struct{})
	closed := make(chan struct{})
	oldDone := make(chan struct{})
	newCalled := make(chan *TrackWriter, 1)

	err := broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		close(started)
		<-closed
	}))
	require.NoError(t, err)

	oldTrackWriter := &TrackWriter{
		TrackName:    "video",
		groupManager: newGroupWriterManager(),
		onCloseTrackFunc: func() {
			close(closed)
		},
	}

	go func() {
		broadcast.ServeTrack(oldTrackWriter)
		close(oldDone)
	}()

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected original handler to start serving track")
	}

	err = broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		newCalled <- tw
	}))
	require.NoError(t, err)

	select {
	case <-oldDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected previous active track to stop after handler replacement")
	}

	newTrackWriter := &TrackWriter{TrackName: "video"}
	broadcast.ServeTrack(newTrackWriter)

	select {
	case got := <-newCalled:
		assert.Same(t, newTrackWriter, got)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected replacement handler to receive track writer")
	}
}

func TestBroadcastClose(t *testing.T) {
	broadcast := NewBroadcast()

	started := make(chan struct{})
	closed := make(chan struct{})
	done := make(chan struct{})

	err := broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		close(started)
		<-closed
	}))
	require.NoError(t, err)

	tw := &TrackWriter{
		TrackName:    "video",
		groupManager: newGroupWriterManager(),
		onCloseTrackFunc: func() {
			close(closed)
		},
	}

	go func() {
		broadcast.ServeTrack(tw)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected handler to start serving track")
	}

	broadcast.Close()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected active track to stop after Close")
	}

	// Handler should be empty; ServeTrack should fall back to NotFoundTrackHandler
	handler := broadcast.Handler("video")
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(handler).Pointer())
}

func TestBroadcastClose_Empty(t *testing.T) {
	broadcast := NewBroadcast()
	broadcast.Close() // should not panic
}

func TestBroadcastClose_Nil(t *testing.T) {
	var broadcast *Broadcast
	broadcast.Close() // should not panic
}

func TestBroadcastClose_MultipleHandlers(t *testing.T) {
	broadcast := NewBroadcast()

	videoClosed := make(chan struct{})
	audioClosed := make(chan struct{})

	videoStarted := make(chan struct{})
	audioStarted := make(chan struct{})

	err := broadcast.Register("video", TrackHandlerFunc(func(tw *TrackWriter) {
		close(videoStarted)
		<-videoClosed
	}))
	require.NoError(t, err)

	err = broadcast.Register("audio", TrackHandlerFunc(func(tw *TrackWriter) {
		close(audioStarted)
		<-audioClosed
	}))
	require.NoError(t, err)

	videoTW := &TrackWriter{
		TrackName:    "video",
		groupManager: newGroupWriterManager(),
		onCloseTrackFunc: func() {
			close(videoClosed)
		},
	}
	audioTW := &TrackWriter{
		TrackName:    "audio",
		groupManager: newGroupWriterManager(),
		onCloseTrackFunc: func() {
			close(audioClosed)
		},
	}

	videoDone := make(chan struct{})
	audioDone := make(chan struct{})

	go func() {
		broadcast.ServeTrack(videoTW)
		close(videoDone)
	}()
	go func() {
		broadcast.ServeTrack(audioTW)
		close(audioDone)
	}()

	select {
	case <-videoStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected video handler to start")
	}
	select {
	case <-audioStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected audio handler to start")
	}

	broadcast.Close()

	select {
	case <-videoDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected video track to stop after Close")
	}
	select {
	case <-audioDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected audio track to stop after Close")
	}

	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(broadcast.Handler("video")).Pointer())
	assert.Equal(t, reflect.ValueOf(NotFoundTrackHandler).Pointer(), reflect.ValueOf(broadcast.Handler("audio")).Pointer())
}


func TestBroadcast_Register(t *testing.T) {
	dummyHandler := TrackHandlerFunc(func(*TrackWriter) {})

	tests := []struct {
		name         string
		broadcast    *Broadcast
		trackName    TrackName
		handler      TrackHandler
		wantErr      string
	}{
		{
			name:      "valid registration",
			broadcast: NewBroadcast(),
			trackName: "video",
			handler:   dummyHandler,
			wantErr:   "",
		},
		{
			name:      "valid registration zero value",
			broadcast: &Broadcast{},
			trackName: "video",
			handler:   dummyHandler,
			wantErr:   "",
		},
		{
			name:      "nil broadcast",
			broadcast: nil,
			trackName: "video",
			handler:   dummyHandler,
			wantErr:   "moqt: nil broadcast",
		},
		{
			name:      "empty track name",
			broadcast: NewBroadcast(),
			trackName: "",
			handler:   dummyHandler,
			wantErr:   "moqt: track name is required",
		},
		{
			name:      "nil handler",
			broadcast: NewBroadcast(),
			trackName: "video",
			handler:   nil,
			wantErr:   "moqt: track handler cannot be nil",
		},
		{
			name:      "typed nil handler func",
			broadcast: NewBroadcast(),
			trackName: "video",
			handler:   TrackHandlerFunc(nil),
			wantErr:   "moqt: track handler function cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.broadcast.Register(tt.trackName, tt.handler)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, tt.broadcast.trackHandlers)
				assert.Contains(t, tt.broadcast.trackHandlers, tt.trackName)
			}
		})
	}
}
