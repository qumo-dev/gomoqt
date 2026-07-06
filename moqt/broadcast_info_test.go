package moqt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcast_RegisterWithInfo_AndTrackInfo(t *testing.T) {
	b := NewBroadcast()

	info := PublishInfo{Priority: 7, Ordered: true, MaxLatency: 500, Timescale: 48000}
	require.NoError(t, b.RegisterWithInfo("audio", info, TrackHandlerFunc(func(*TrackWriter) {})))
	require.NoError(t, b.Register("video", TrackHandlerFunc(func(*TrackWriter) {})))

	t.Run("registered with info returns it", func(t *testing.T) {
		got, ok := b.TrackInfo("audio")
		require.True(t, ok)
		assert.Equal(t, info, got)
	})

	t.Run("registered without info returns zero-value info (found)", func(t *testing.T) {
		got, ok := b.TrackInfo("video")
		require.True(t, ok)
		assert.Equal(t, PublishInfo{}, got)
	})

	t.Run("unknown track not found", func(t *testing.T) {
		_, ok := b.TrackInfo("missing")
		assert.False(t, ok)
	})

	t.Run("empty name not found", func(t *testing.T) {
		_, ok := b.TrackInfo("")
		assert.False(t, ok)
	})

	t.Run("nil broadcast not found", func(t *testing.T) {
		var nilB *Broadcast
		_, ok := nilB.TrackInfo("audio")
		assert.False(t, ok)
	})
}
