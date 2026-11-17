package msf

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastRegisterTrack_UpdatesCatalogAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
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
	require.Len(t, handler.servedTracks(), 1)
	assert.Equal(t, tw, handler.servedTracks()[0])
}

func TestBroadcastHandler_ResolvesCatalogAndTrackHandlers(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Name:      "audio",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	tw := &moqt.TrackWriter{TrackName: "audio"}
	broadcast.Handler("audio").ServeTrack(tw)
	require.Len(t, handler.servedTracks(), 1)
	assert.Equal(t, tw, handler.servedTracks()[0])
	broadcast.Handler("missing").ServeTrack(tw)
	assert.Len(t, handler.servedTracks(), 1)
}

func TestBroadcastRemoveTrack_RemovesCatalogEntryAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	require.NoError(t, broadcast.RemoveTrack("video"))
	assert.ErrorIs(t, broadcast.RemoveTrack("video"), ErrTrackNotFound)
	assert.Empty(t, broadcast.Catalog().Tracks)
	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	assert.Empty(t, handler.servedTracks())
}

func TestBroadcastCatalogBytes_EncodesCurrentCatalog(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	handler := &fakeTrackHandler{}

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
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	handler := &fakeTrackHandler{}

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
			handler:      &fakeTrackHandler{},
			errorMessage: "track name is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
			require.NoError(t, err)

			err = broadcast.RegisterTrack(tt.track, tt.handler)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestBroadcastRegisterTrack_ReplacesExistingTrackAndHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	firstHandler := &fakeTrackHandler{}
	secondHandler := &fakeTrackHandler{}

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
	require.Len(t, secondHandler.servedTracks(), 1)
	assert.Equal(t, tw, secondHandler.servedTracks()[0])
}

func TestBroadcastZeroValue_UsesDefaultCatalogTrackName(t *testing.T) {
	var broadcast Broadcast

	assert.Equal(t, DefaultCatalogTrackName, broadcast.CatalogTrackName())
	assert.NotNil(t, broadcast.Handler(DefaultCatalogTrackName))
	assert.Zero(t, broadcast.Catalog().Version)
	assert.Empty(t, broadcast.Catalog().Tracks)

	require.NoError(t, broadcast.SetCatalog(Catalog{Version: CurrentVersion}))
	assert.Equal(t, DefaultCatalogTrackName, broadcast.CatalogTrackName())
	assert.Equal(t, CurrentVersion, broadcast.Catalog().Version)
	assert.NotNil(t, broadcast.Handler(DefaultCatalogTrackName))
	assert.ErrorContains(t, broadcast.RemoveTrack(DefaultCatalogTrackName), "reserved for the catalog track")
	assert.ErrorContains(t, broadcast.RemoveTrack(""), "track name is required")
	handler := &fakeTrackHandler{}
	assert.Error(t, broadcast.RegisterTrack(Track{Name: string(DefaultCatalogTrackName)}, handler))
}

func TestBroadcastSetCatalog_PrunesRemovedTrackHandlers(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{Version: CurrentVersion, Tracks: []Track{{
		Name:      "audio",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.NoError(t, err)

	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.Handler("video").ServeTrack(tw)
	assert.Empty(t, handler.servedTracks())
}

func TestBroadcastSetCatalog_RejectsReservedCatalogTrackName(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{Version: CurrentVersion, Tracks: []Track{{
		Name:      string(DefaultCatalogTrackName),
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved track name")
}

func TestNewBroadcast_RejectsReservedCatalogTrackName(t *testing.T) {
	_, err := NewBroadcast(Catalog{Version: CurrentVersion, Tracks: []Track{{
		Name:      string(DefaultCatalogTrackName),
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved track name")
}

func TestBroadcast_ServeTrack_NilTrackWriter(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	// Should not panic with nil TrackWriter.
	broadcast.ServeTrack(nil)
}

func TestBroadcast_ServeTrack_DispatchesToHandler(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.NoError(t, err)

	tw := &moqt.TrackWriter{TrackName: "video"}
	broadcast.ServeTrack(tw)
	require.Len(t, handler.servedTracks(), 1)
	assert.Equal(t, tw, handler.servedTracks()[0])
}

func TestBroadcast_NilReceiver(t *testing.T) {
	var broadcast *Broadcast

	assert.Equal(t, DefaultCatalogTrackName, broadcast.CatalogTrackName())
	assert.Equal(t, Catalog{}, broadcast.Catalog())

	_, err := broadcast.CatalogBytes()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil broadcast")

	err = broadcast.SetCatalog(Catalog{Version: CurrentVersion})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil broadcast")

	err = broadcast.RegisterTrack(Track{Name: "video"}, &fakeTrackHandler{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil broadcast")

	assert.ErrorContains(t, broadcast.RemoveTrack("video"), "nil broadcast")

	h := broadcast.Handler("video")
	assert.Equal(t, reflect.ValueOf(moqt.NotFoundTrackHandler).Pointer(), reflect.ValueOf(h).Pointer())
}

func TestBroadcast_SetCatalog_RejectsInvalidCatalog(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog version is required")
}

func TestBroadcast_RegisterTrack_RejectsInvalidCatalogAfterAdd(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Name: "video",
	}, handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "packaging is required")
}

func TestBroadcast_staleTrackNamesLocked_EmptyCurrent(t *testing.T) {
	broadcast := &Broadcast{}
	broadcast.catalog = Catalog{}

	stale := broadcast.staleTrackNamesLocked(Catalog{Tracks: []Track{{Name: "video"}}})
	assert.Nil(t, stale)
}

func TestBroadcastSetCatalog_RejectsDuplicateTrackNamesAcrossNamespaces(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	err = broadcast.SetCatalog(Catalog{
		Version:          CurrentVersion,
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
		Version:          CurrentVersion,
		DefaultNamespace: "live/main",
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(false),
		}},
	})
	require.NoError(t, err)

	handler := &fakeTrackHandler{}
	err = broadcast.RegisterTrack(Track{
		Namespace: "live/backup",
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(false),
	}, handler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unique track names across namespaces")
}

// TestBroadcast_RemoveTrack_KeepsCatalogValid checks that RemoveTrack refuses
// a removal that would leave a dangling reference, and leaves the catalog and
// the handler as they were.
func TestBroadcast_RemoveTrack_KeepsCatalogValid(t *testing.T) {
	video := Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true), RenderGroup: new(int64(1))}
	audio := Track{Name: "audio", Packaging: PackagingLOC, IsLive: new(true), RenderGroup: new(int64(1))}
	byName := Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{DependsOnTrack("video")}}
	byGroup := Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{DependsOnRenderGroup(1)}}

	tests := map[string]struct {
		tracks   []Track // registered in this order
		remove   moqt.TrackName
		errMatch string
	}{
		"track named by depends": {
			tracks:   []Track{video, byName},
			remove:   "video",
			errMatch: `track name "video" does not match any other track`,
		},
		"last track of a depended-on render group": {
			tracks:   []Track{video, byGroup},
			remove:   "video",
			errMatch: "renderGroup 1 does not match any track",
		},
		"render group keeps another member": {
			tracks: []Track{video, audio, byGroup},
			remove: "video",
		},
		"dependent track itself": {
			tracks: []Track{video, byName},
			remove: "timeline",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
			require.NoError(t, err)
			handler := &fakeTrackHandler{}
			for _, track := range tt.tracks {
				require.NoError(t, broadcast.RegisterTrack(track, handler))
			}
			before := broadcast.Catalog()

			err = broadcast.RemoveTrack(tt.remove)

			if tt.errMatch == "" {
				require.NoError(t, err)
				assert.Len(t, broadcast.Catalog().Tracks, len(tt.tracks)-1)
				require.NoError(t, broadcast.Catalog().Validate())
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
			var validationErr *ValidationError
			assert.ErrorAs(t, err, &validationErr)
			assert.Equal(t, before, broadcast.Catalog(), "a refused removal leaves the catalog unchanged")
			broadcast.Handler(tt.remove).ServeTrack(&moqt.TrackWriter{TrackName: tt.remove})
			assert.Len(t, handler.servedTracks(), 1, "a refused removal keeps the handler")
			// The catalog stayed valid, so later registrations still work.
			require.NoError(t, broadcast.RegisterTrack(Track{Name: "extra", Packaging: PackagingLOC, IsLive: new(true)}, handler))
		})
	}
}

func TestBroadcast_RegisterTrack_RequiresDependencyOrder(t *testing.T) {
	broadcast, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)
	handler := &fakeTrackHandler{}

	timeline := Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{DependsOnTrack("video")}}
	require.Error(t, broadcast.RegisterTrack(timeline, handler), "a track cannot register before what it depends on")

	require.NoError(t, broadcast.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, handler))
	require.NoError(t, broadcast.RegisterTrack(timeline, handler))
}
