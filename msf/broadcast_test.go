package msf

import (
	"encoding/json"
	"testing"

	"github.com/okdaichi/gomoqt/moqt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastRegisterTrack_UpdatesCatalogAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	servedTracks := make([]*moqt.TrackWriter, 0)
	handler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			servedTracks = append(servedTracks, tw)
		},
	}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	catalog := broadcast.Catalog()
	require.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "video", catalog.Tracks[0].Name)
	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	require.Len(t, servedTracks, 1)
	assert.Equal(t, tw, servedTracks[0])
}

func TestBroadcastHandler_ResolvesCatalogAndTrackHandlers(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	servedTracks := make([]*moqt.TrackWriter, 0)
	handler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			servedTracks = append(servedTracks, tw)
		},
	}
	err = broadcast.RegisterTrack(Track{
		Name:      "audio",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	tw := &moqt.TrackWriter{TrackName: "audio"}
	broadcast.Handler("audio").ServeTrack(tw)
	require.Len(t, servedTracks, 1)
	assert.Equal(t, tw, servedTracks[0])
	broadcast.Handler("missing").ServeTrack(tw)
	assert.Len(t, servedTracks, 1)
}

func TestBroadcastRemoveTrack_RemovesCatalogEntryAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	servedTracks := make([]*moqt.TrackWriter, 0)
	handler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			servedTracks = append(servedTracks, tw)
		},
	}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	removed := broadcast.RemoveTrack("video")
	assert.True(t, removed)
	assert.Empty(t, broadcast.Catalog().Tracks)
	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	assert.Empty(t, servedTracks)
}

func TestBroadcastCatalogBytes_EncodesCurrentCatalog(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)
	handler := &FakeTrackHandler{}

	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	data, err := broadcast.CatalogBytes()
	require.NoError(t, err)

	var decoded Catalog
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded.Tracks, 1)
	assert.Equal(t, "video", decoded.Tracks[0].Name)
}

func TestBroadcastRegisterTrack_RejectsReservedCatalogTrackName(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)
	handler := &FakeTrackHandler{}

	err = broadcast.RegisterTrack(Track{
		Name:      string(DefaultCatalogTrackName),
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestBroadcastRegisterTrack_RejectsInvalidInput(t *testing.T) {
	tests := map[string]struct {
		track        Track
		handler      moqt.TrackHandler
		errorMessage string
	}{
		"nil handler": {
			track:        Track{Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
			handler:      nil,
			errorMessage: "track handler cannot be nil",
		},
		"nil handler func": {
			track:        Track{Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
			handler:      moqt.TrackHandlerFunc(nil),
			errorMessage: "track handler function cannot be nil",
		},
		"empty track name": {
			track:        Track{Packaging: PackagingLOC, IsLive: new(false)},
			handler:      &FakeTrackHandler{},
			errorMessage: "track name is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			broadcast, err := NewBroadcast(Catalog{Version: 1})
			require.NoError(t, err)

			err = broadcast.RegisterTrack(tt.track, tt.handler)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestBroadcastRegisterTrack_ReplacesExistingTrackAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	firstServedTracks := make([]*moqt.TrackWriter, 0)
	firstHandler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			firstServedTracks = append(firstServedTracks, tw)
		},
	}
	secondServedTracks := make([]*moqt.TrackWriter, 0)
	secondHandler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			secondServedTracks = append(secondServedTracks, tw)
		},
	}

	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(true),
		Label:     "primary",
	}, firstHandler)
	require.NoError(t, err)

	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
		Label:     "replacement",
	}, secondHandler)
	require.NoError(t, err)

	catalog := broadcast.Catalog()
	require.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "replacement", catalog.Tracks[0].Label)
	require.NotNil(t, catalog.Tracks[0].IsLive)
	assert.False(t, *catalog.Tracks[0].IsLive)
	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	require.Len(t, secondServedTracks, 1)
	assert.Equal(t, tw, secondServedTracks[0])
}

func TestBroadcastZeroValue_UsesDefaultCatalogTrackName(t *testing.T) {
	var broadcast Broadcast

	assert.Equal(t, DefaultCatalogTrackName, broadcast.CatalogTrackName())
	assert.NotNil(t, broadcast.Handler(DefaultCatalogTrackName))
	assert.Zero(t, broadcast.Catalog().Version)
	assert.Empty(t, broadcast.Catalog().Tracks)

	require.NoError(t, broadcast.SetCatalog(Catalog{Version: 1}))
	assert.Equal(t, DefaultCatalogTrackName, broadcast.CatalogTrackName())
	assert.Equal(t, 1, broadcast.Catalog().Version)
	assert.NotNil(t, broadcast.Handler(DefaultCatalogTrackName))
	assert.False(t, broadcast.RemoveTrack(DefaultCatalogTrackName))
	assert.False(t, broadcast.RemoveTrack(""))
	servedTracks := make([]*moqt.TrackWriter, 0)
	handler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			servedTracks = append(servedTracks, tw)
		},
	}
	assert.Error(t, broadcast.RegisterTrack(Track{Name: string(DefaultCatalogTrackName)}, handler))
}

func TestBroadcastSetCatalog_PrunesRemovedTrackHandlers(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	servedTracks := make([]*moqt.TrackWriter, 0)
	handler := &FakeTrackHandler{
		ServeTrackFunc: func(tw *moqt.TrackWriter) {
			servedTracks = append(servedTracks, tw)
		},
	}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{Version: 1, Tracks: []Track{{
		Name:      "audio",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.NoError(t, err)

	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	assert.Empty(t, servedTracks)
}

func TestBroadcastSetCatalog_RejectsReservedCatalogTrackName(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{Version: 1, Tracks: []Track{{
		Name:      string(DefaultCatalogTrackName),
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved track name")
}

func TestNewBroadcast_RejectsReservedCatalogTrackName(t *testing.T) {
	_, err := NewBroadcast(Catalog{Version: 1, Tracks: []Track{{
		Name:      string(DefaultCatalogTrackName),
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved track name")
}

func TestBroadcastSetCatalog_RejectsDuplicateTrackNamesAcrossNamespaces(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: 1})
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{
		Version:          1,
		DefaultNamespace: "live/main",
		Tracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
			{Namespace: "live/backup", Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique track names across namespaces")
}

func TestBroadcastRegisterTrack_RejectsDuplicateTrackNamesAcrossNamespaces(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{
		Version:          1,
		DefaultNamespace: "live/main",
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(false),
		}},
	})
	require.NoError(t, err)

	handler := &FakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Namespace: "live/backup",
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique track names across namespaces")
}
