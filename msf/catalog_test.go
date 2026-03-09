package msf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackaging_StringAndKnown(t *testing.T) {
	tests := map[string]struct {
		packaging Packaging
		expected  string
		known     bool
	}{
		"loc": {
			packaging: PackagingLOC,
			expected:  "loc",
			known:     true,
		},
		"media timeline": {
			packaging: PackagingMediaTimeline,
			expected:  "mediatimeline",
			known:     true,
		},
		"custom": {
			packaging: Packaging("custom"),
			expected:  "custom",
			known:     false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.packaging.String())
			assert.Equal(t, tt.known, tt.packaging.IsKnown())
		})
	}
}

func TestRole_StringAndKnown(t *testing.T) {
	tests := map[string]struct {
		role     Role
		expected string
		known    bool
	}{
		"video": {
			role:     RoleVideo,
			expected: "video",
			known:    true,
		},
		"audio": {
			role:     RoleAudio,
			expected: "audio",
			known:    true,
		},
		"custom": {
			role:     Role("custom"),
			expected: "custom",
			known:    false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.String())
			assert.Equal(t, tt.known, tt.role.IsKnown())
		})
	}
}

func TestCatalogValidate_Errors(t *testing.T) {
	tests := map[string]struct {
		catalog      Catalog
		errorMessage string
	}{
		"duplicate track identity": {
			catalog: Catalog{
				Version:          1,
				DefaultNamespace: "live/demo",
				Tracks: []Track{
					{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
					{Namespace: "live/demo", Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
				},
			},
			errorMessage: "duplicate track identity",
		},
		"loc packaging requires isLive": {
			catalog: Catalog{
				Version: 1,
				Tracks:  []Track{{Name: "video", Packaging: PackagingLOC}},
			},
			errorMessage: "isLive is required for loc tracks",
		},
		"live track rejects duration": {
			catalog: Catalog{
				Version: 1,
				Tracks: []Track{{
					Name:          "video",
					Packaging:     PackagingLOC,
					IsLive:        new(true),
					TrackDuration: new(int64(1)),
				}},
			},
			errorMessage: "trackDuration must not be present when isLive is true",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.catalog.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestCatalogValidate_RequiresVersionForIndependentCatalog(t *testing.T) {
	catalog := Catalog{
		Tracks: []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog version is required")
}

func TestCatalogValidate_TimelineRequirements(t *testing.T) {
	catalog := Catalog{
		Version: 1,
		Tracks: []Track{
			{Name: "timeline", Packaging: PackagingMediaTimeline},
			{Name: "events", Packaging: PackagingEventTimeline, MimeType: "application/json", Depends: []string{"video"}},
		},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mediatimeline tracks must use mimeType application/json")
	assert.Contains(t, err.Error(), "mediatimeline tracks must declare depends")
	assert.Contains(t, err.Error(), "eventType is required for eventtimeline tracks")
}

func TestCatalogRoundTrip_PreservesCustomFields(t *testing.T) {
	input := []byte(`{
		"version": 1,
		"generatedAt": 1234,
		"com.example.catalog": "premium",
		"tracks": [
			{
				"name": "video",
				"packaging": "loc",
				"isLive": true,
				"com.example.track": 7
			}
		]
	}`)

	var catalog Catalog
	require.NoError(t, json.Unmarshal(input, &catalog))
	require.Contains(t, catalog.ExtraFields, "com.example.catalog")
	require.Contains(t, catalog.Tracks[0].ExtraFields, "com.example.track")

	output, err := json.Marshal(catalog)
	require.NoError(t, err)
	assert.Contains(t, string(output), `"com.example.catalog":"premium"`)
	assert.Contains(t, string(output), `"com.example.track":7`)
}

func TestParseCatalogString_RoundTrip(t *testing.T) {
	input := `{
		"version": 1,
		"tracks": [
			{"name": "video", "packaging": "loc", "isLive": true}
		]
	}`

	catalog, err := ParseCatalogString(input)
	require.NoError(t, err)
	assert.Equal(t, 1, catalog.Version)
	require.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "video", catalog.Tracks[0].Name)
	assert.Equal(t, PackagingLOC, catalog.Tracks[0].Packaging)
	require.NotNil(t, catalog.Tracks[0].IsLive)
	assert.True(t, *catalog.Tracks[0].IsLive)
}

func TestParseCatalog_RejectsDeltaJSON(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"deltaUpdate": true, "addTracks": [{"name": "video", "packaging": "loc", "isLive": true}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog fields are not allowed")
}

func TestParseCatalog_RejectsTrailingJSON(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"version":1} {"extra":2}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after top-level value")
}

func TestParseCatalogDelta_RoundTrip(t *testing.T) {
	input := `{
		"deltaUpdate": true,
		"addTracks": [
			{"name": "video", "packaging": "loc", "isLive": true}
		]
	}`

	delta, err := ParseCatalogDeltaString(input)
	require.NoError(t, err)
	require.Len(t, delta.AddTracks, 1)
	assert.Equal(t, "video", delta.AddTracks[0].Name)
	assert.Equal(t, PackagingLOC, delta.AddTracks[0].Packaging)
	require.NotNil(t, delta.AddTracks[0].IsLive)
	assert.True(t, *delta.AddTracks[0].IsLive)
}

func TestParseCatalogDelta_RejectsIndependentJSON(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"version": 1, "tracks": [{"name": "video", "packaging": "loc", "isLive": true}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "independent catalog fields are not allowed")
}

func TestParseCatalogDelta_RejectsTrailingJSON(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"deltaUpdate":true,"addTracks":[{"name":"video","packaging":"loc","isLive":true}]} {"extra":2}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after top-level value")
}

func TestCatalogApplyDelta_PreservesDeclaredOperationOrder(t *testing.T) {
	base := Catalog{
		Version: 1,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true), Bitrate: new(int64(1000))}},
	}

	var delta CatalogDelta
	require.NoError(t, json.Unmarshal([]byte(`{
		"deltaUpdate": true,
		"removeTracks": [{"name": "video"}],
		"addTracks": [{"name": "video", "packaging": "loc", "isLive": true, "bitrate": 2000}]
	}`), &delta))

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	require.Len(t, updated.Tracks, 1)
	require.NotNil(t, updated.Tracks[0].Bitrate)
	assert.Equal(t, int64(2000), *updated.Tracks[0].Bitrate)
}

func TestCatalogApplyDelta_CloneTrackInheritsParent(t *testing.T) {
	base := Catalog{
		Version: 1,
		Tracks: []Track{{
			Name:        "video-1080",
			Packaging:   PackagingLOC,
			IsLive:      new(true),
			Codec:       "av01",
			Width:       new(int64(1920)),
			Height:      new(int64(1080)),
			Bitrate:     new(int64(5000000)),
			RenderGroup: new(int64(1)),
		}},
	}
	delta := CatalogDelta{
		CloneTracks: []TrackClone{{
			Track: Track{
				Name:    "video-720",
				Width:   new(int64(1280)),
				Height:  new(int64(720)),
				Bitrate: new(int64(3000000)),
			},
			ParentName: "video-1080",
		}},
	}

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	require.Len(t, updated.Tracks, 2)
	clone := updated.Tracks[1]
	assert.Equal(t, "video-720", clone.Name)
	assert.Equal(t, "av01", clone.Codec)
	assert.Equal(t, int64(1280), *clone.Width)
	assert.Equal(t, int64(720), *clone.Height)
	assert.Equal(t, int64(3000000), *clone.Bitrate)
	assert.Equal(t, int64(1), *clone.RenderGroup)
}

func TestCatalogApplyDelta_Errors(t *testing.T) {
	tests := map[string]struct {
		base         Catalog
		delta        CatalogDelta
		errorMessage string
	}{
		"remove unknown track": {
			base: Catalog{
				Version: 1,
				Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
			},
			delta: CatalogDelta{
				RemoveTracks: []TrackRef{{Name: "audio"}},
			},
			errorMessage: "cannot remove unknown track",
		},
		"clone unknown parent": {
			base: Catalog{
				Version: 1,
				Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
			},
			delta: CatalogDelta{
				CloneTracks: []TrackClone{{Track: Track{Name: "audio"}, ParentName: "missing"}},
			},
			errorMessage: "cannot clone unknown parent track",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := tt.base.ApplyDelta(tt.delta)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestCatalogApplyDelta_MergesMetadata(t *testing.T) {
	generatedAt := int64(1234)
	base := Catalog{
		Version:          1,
		DefaultNamespace: "live/demo",
		Tracks:           []Track{{Namespace: "live/demo", Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
		ExtraFields:      map[string]json.RawMessage{"base": json.RawMessage(`true`)},
	}
	delta := CatalogDelta{
		DefaultNamespace: "live/updated",
		GeneratedAt:      &generatedAt,
		IsComplete:       true,
		ExtraFields:      map[string]json.RawMessage{"delta": json.RawMessage(`{"ok":true}`)},
		AddTracks:        []Track{{Name: "audio", Packaging: PackagingLOC, IsLive: new(false)}},
	}

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	assert.Equal(t, "live/updated", updated.DefaultNamespace)
	require.NotNil(t, updated.GeneratedAt)
	assert.Equal(t, generatedAt, *updated.GeneratedAt)
	assert.True(t, updated.IsComplete)
	assert.Contains(t, updated.ExtraFields, "base")
	assert.Contains(t, updated.ExtraFields, "delta")
	require.Len(t, updated.Tracks, 2)
	assert.Equal(t, "audio", updated.Tracks[1].Name)
}

func TestCatalogApplyDelta_RejectsChangingDefaultNamespaceForInheritedTracks(t *testing.T) {
	base := Catalog{
		Version:          1,
		DefaultNamespace: "live/demo",
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
		}},
	}
	delta := CatalogDelta{
		DefaultNamespace: "live/updated",
		AddTracks: []Track{{
			Namespace: "live/updated",
			Name:      "audio",
			Packaging: PackagingLOC,
			IsLive:    new(false),
		}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot change default namespace")
}

func TestCatalogDeltaValidate_RemoveTracksRejectExtraFields(t *testing.T) {
	var delta CatalogDelta
	require.NoError(t, json.Unmarshal([]byte(`{
		"deltaUpdate": true,
		"removeTracks": [{"name": "video", "codec": "av01"}]
	}`), &delta))

	err := delta.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove track entries may contain only name and optional namespace")
}
