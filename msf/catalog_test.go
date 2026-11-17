package msf

import (
	"encoding/json"
	"strings"
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
				Version:          CurrentVersion,
				DefaultNamespace: "live/demo",
				Tracks: []Track{
					{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
					{Namespace: "live/demo", Name: "video", Packaging: PackagingLOC, IsLive: new(false)},
				},
			},
			errorMessage: "duplicate track identity",
		},
		"every track requires isLive": {
			catalog: Catalog{
				Version: CurrentVersion,
				Tracks:  []Track{{Name: "events", Packaging: PackagingEventTimeline, EventType: "com.example.e"}},
			},
			errorMessage: "isLive is required",
		},
		"live track rejects duration": {
			catalog: Catalog{
				Version: CurrentVersion,
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
		Version: CurrentVersion,
		Tracks: []Track{
			{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true)},
			{Name: "events", Packaging: PackagingEventTimeline, IsLive: new(true)},
		},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mediatimeline tracks must declare depends")
	assert.Contains(t, err.Error(), "eventType is required for eventtimeline tracks")
	assert.NotContains(t, err.Error(), "mimeType")
}

func TestCatalogRoundTrip_PreservesCustomFields(t *testing.T) {
	input := []byte(`{
		"version": "draft-02",
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
		"version": "draft-02",
		"tracks": [
			{"name": "video", "packaging": "loc", "isLive": true}
		]
	}`

	catalog, err := ParseCatalogString(input)
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, catalog.Version)
	require.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "video", catalog.Tracks[0].Name)
	assert.Equal(t, PackagingLOC, catalog.Tracks[0].Packaging)
	require.NotNil(t, catalog.Tracks[0].IsLive)
	assert.True(t, *catalog.Tracks[0].IsLive)
}

func TestParseCatalog_RejectsDeltaJSON(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[{"name": "video", "packaging": "loc", "isLive": true}]}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog fields are not allowed")
}

func TestParseCatalog_RejectsTrailingJSON(t *testing.T) {
	_, err := ParseCatalog([]byte(`{"version": "draft-02"} {"extra":2}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after top-level value")
}

func TestParseCatalogDelta_RoundTrip(t *testing.T) {
	input := `{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [
				{"name": "video", "packaging": "loc", "isLive": true}
			]}
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

func TestParseCatalogDelta_ValidJSON(t *testing.T) {
	input := []byte(`{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [
				{"name": "video", "packaging": "loc", "isLive": true}
			]}
		]
	}`)

	delta, err := ParseCatalogDelta(input)
	require.NoError(t, err)
	require.Len(t, delta.AddTracks, 1)
	assert.Equal(t, "video", delta.AddTracks[0].Name)
	assert.Equal(t, PackagingLOC, delta.AddTracks[0].Packaging)
	require.NotNil(t, delta.AddTracks[0].IsLive)
	assert.True(t, *delta.AddTracks[0].IsLive)
}

func TestParseCatalogDelta_InvalidJSON(t *testing.T) {
	input := []byte(`{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [
				{"name": "video", "packaging": "loc", "isLive": true
			]}
		]
	}`)

	_, err := ParseCatalogDelta(input)
	require.Error(t, err)
}

func TestParseCatalogDelta_RejectsIndependentJSON(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"version": "draft-02", "tracks": [{"name": "video", "packaging": "loc", "isLive": true}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not allowed in a delta catalog")
}

func TestParseCatalogDelta_RejectsTrailingJSON(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"deltaUpdate":[{"op":"add","tracks":[{"name":"video","packaging":"loc","isLive":true}]}]} {"extra":2}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after top-level value")
}

// Repeated same-type ops are legal in draft-01 and must accumulate, not
// overwrite. encoding/json resets a destination slice before appending, so the
// decoder must unmarshal into a fresh slice and append (see decodeDeltaOps).
func TestParseCatalogDelta_RepeatedSameTypeOpsAccumulate(t *testing.T) {
	delta, err := ParseCatalogDeltaString(`{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [{"name": "a", "packaging": "loc", "isLive": true}]},
			{"op": "remove", "tracks": [{"name": "old"}]},
			{"op": "add", "tracks": [{"name": "b", "packaging": "loc", "isLive": true}]}
		]
	}`)
	require.NoError(t, err)
	require.Len(t, delta.AddTracks, 2)
	assert.Equal(t, "a", delta.AddTracks[0].Name)
	assert.Equal(t, "b", delta.AddTracks[1].Name)
	require.Len(t, delta.RemoveTracks, 1)
	assert.Equal(t, "old", delta.RemoveTracks[0].Name)
}

// TestCatalogApplyDelta_InterleavedOperations verifies interleaved same-type
// operations apply in wire order, not grouped by type: remove, add, remove of
// one track leaves it removed. Grouped, the second remove would run before the
// add and fail on an unknown track.
func TestCatalogApplyDelta_InterleavedOperations(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)},
		},
	}
	delta, err := ParseCatalogDeltaString(`{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "remove", "tracks": [{"name": "video"}]},
			{"op": "add", "tracks": [{"name": "video", "packaging": "loc", "isLive": true}]},
			{"op": "remove", "tracks": [{"name": "video"}]}
		]
	}`)
	require.NoError(t, err)

	updated, err := base.ApplyDelta(delta)

	require.NoError(t, err)
	require.Len(t, updated.Tracks, 1)
	assert.Equal(t, "audio", updated.Tracks[0].Name)
}

// TestCatalogDelta_MarshalJSON_Operations verifies the encoded deltaUpdate
// array keeps decoded operations in wire order, and that tracks added in code
// beyond them follow as one operation of their kind.
func TestCatalogDelta_MarshalJSON_Operations(t *testing.T) {
	const wire = `{"deltaUpdate":[` +
		`{"op":"remove","tracks":[{"name":"a"}]},` +
		`{"op":"add","tracks":[{"isLive":true,"name":"a","packaging":"loc"}]},` +
		`{"op":"remove","tracks":[{"name":"a"}]}]}`

	tests := map[string]struct {
		extend func(*CatalogDelta)
		ops    []string
	}{
		"decoded":          {ops: []string{"remove", "add", "remove"}},
		"extended in code": {extend: func(d *CatalogDelta) { d.AddTracks = append(d.AddTracks, Track{Name: "b"}) }, ops: []string{"remove", "add", "remove", "add"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			delta, err := ParseCatalogDeltaString(wire)
			require.NoError(t, err)
			if tt.extend != nil {
				tt.extend(&delta)
			}

			data, err := json.Marshal(delta)
			require.NoError(t, err)

			var got struct {
				DeltaUpdate []struct {
					Op string `json:"op"`
				} `json:"deltaUpdate"`
			}
			require.NoError(t, json.Unmarshal(data, &got))
			ops := make([]string, 0, len(got.DeltaUpdate))
			for _, op := range got.DeltaUpdate {
				ops = append(ops, op.Op)
			}
			assert.Equal(t, tt.ops, ops)
		})
	}
}

func TestCatalogApplyDelta_PreservesDeclaredOperationOrder(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true), Bitrate: new(int64(1000))}},
	}

	var delta CatalogDelta
	require.NoError(t, json.Unmarshal([]byte(`{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "remove", "tracks": [{"name": "video"}]},
			{"op": "add", "tracks": [{"name": "video", "packaging": "loc", "isLive": true, "bitrate": 2000}]}
		]
	}`), &delta))

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	require.Len(t, updated.Tracks, 1)
	require.NotNil(t, updated.Tracks[0].Bitrate)
	assert.Equal(t, int64(2000), *updated.Tracks[0].Bitrate)
}

func TestCatalogApplyDelta_CloneTrackInheritsParent(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
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
		GeneratedAt: new(int64(1)),
		CloneTracks: []TrackClone{{
			Name:       "video-720",
			Width:      new(int64(1280)),
			Height:     new(int64(720)),
			Bitrate:    new(int64(3000000)),
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
				Version: CurrentVersion,
				Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
			},
			delta: CatalogDelta{
				GeneratedAt:  new(int64(1)),
				RemoveTracks: []TrackRef{{Name: "audio"}},
			},
			errorMessage: "cannot remove unknown track",
		},
		"clone unknown parent": {
			base: Catalog{
				Version: CurrentVersion,
				Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
			},
			delta: CatalogDelta{
				GeneratedAt: new(int64(1)),
				CloneTracks: []TrackClone{{Name: "audio", ParentName: "missing"}},
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
		Version:          CurrentVersion,
		DefaultNamespace: "live/demo",
		Tracks:           []Track{{Namespace: "live/demo", Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
		ExtraFields:      map[string]json.RawMessage{"base": json.RawMessage(`true`)},
	}
	delta := CatalogDelta{
		DefaultNamespace: "live/updated",
		GeneratedAt:      &generatedAt,
		ExtraFields:      map[string]json.RawMessage{"delta": json.RawMessage(`{"ok":true}`)},
		AddTracks:        []Track{{Name: "audio", Packaging: PackagingLOC, IsLive: new(false)}},
	}

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	assert.Equal(t, "live/updated", updated.DefaultNamespace)
	require.NotNil(t, updated.GeneratedAt)
	assert.Equal(t, generatedAt, *updated.GeneratedAt)
	assert.False(t, updated.IsComplete)
	assert.Contains(t, updated.ExtraFields, "base")
	assert.Contains(t, updated.ExtraFields, "delta")
	require.Len(t, updated.Tracks, 2)
	assert.Equal(t, "audio", updated.Tracks[1].Name)
}

func TestCatalogApplyDelta_RejectsChangingDefaultNamespaceForInheritedTracks(t *testing.T) {
	base := Catalog{
		Version:          CurrentVersion,
		DefaultNamespace: "live/demo",
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
		}},
	}
	delta := CatalogDelta{
		GeneratedAt:      new(int64(1)),
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
		"generatedAt": 1, "deltaUpdate": [
			{"op": "remove", "tracks": [{"name": "video", "codec": "av01"}]}
		]
	}`), &delta))

	err := delta.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove track entries may contain only name and optional namespace")
}

func TestValidationError_EmptyProblems(t *testing.T) {
	err := &ValidationError{}
	assert.Equal(t, "msf: validation failed", err.Error())
}

func TestTrackID_String(t *testing.T) {
	tests := map[string]struct {
		id       TrackID
		expected string
	}{
		"with namespace": {
			id:       TrackID{Namespace: "live/demo", Name: "video"},
			expected: "live/demo/video",
		},
		"empty namespace": {
			id:       TrackID{Namespace: "", Name: "video"},
			expected: "video",
		},
		"inherited namespace sentinel": {
			id:       TrackID{Namespace: inheritedNamespaceSentinel, Name: "video"},
			expected: "video",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.id.String())
		})
	}
}

func TestTrackRoundTrip_AllFields(t *testing.T) {
	track := Track{
		Namespace:     "live/demo",
		Name:          "video",
		Packaging:     PackagingLOC,
		EventType:     "",
		Role:          RoleVideo,
		IsLive:        new(true),
		TargetLatency: new(int64(500)),
		Label:         "HD",
		RenderGroup:   new(int64(1)),
		AltGroup:      new(int64(2)),
		InitRef:       "init1",
		Depends:       []Dependency{{TrackName: "audio"}},
		TemporalID:    new(int64(3)),
		SpatialID:     new(int64(1)),
		Codec:         "av01",
		MimeType:      "video/mp4",
		Framerate:     new(int64(30)),
		Timescale:     new(int64(90000)),
		Bitrate:       new(int64(5000000)),
		Width:         new(int64(1920)),
		Height:        new(int64(1080)),
		SampleRate:    new(int64(48000)),
		ChannelConfig: "stereo",
		DisplayWidth:  new(int64(1920)),
		DisplayHeight: new(int64(1080)),
		Language:      "en",
		TrackDuration: nil,
	}

	data, err := json.Marshal(track)
	require.NoError(t, err)

	var decoded Track
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, track.Namespace, decoded.Namespace)
	assert.Equal(t, track.Name, decoded.Name)
	assert.Equal(t, track.Packaging, decoded.Packaging)
	assert.Equal(t, track.Role, decoded.Role)
	require.NotNil(t, decoded.IsLive)
	assert.True(t, *decoded.IsLive)
	assert.Equal(t, int64(500), *decoded.TargetLatency)
	assert.Equal(t, "HD", decoded.Label)
	assert.Equal(t, int64(1), *decoded.RenderGroup)
	assert.Equal(t, int64(2), *decoded.AltGroup)
	assert.Equal(t, "init1", decoded.InitRef)
	assert.Equal(t, []Dependency{{TrackName: "audio"}}, decoded.Depends)
	assert.Equal(t, int64(3), *decoded.TemporalID)
	assert.Equal(t, int64(1), *decoded.SpatialID)
	assert.Equal(t, "av01", decoded.Codec)
	assert.Equal(t, "video/mp4", decoded.MimeType)
	assert.Equal(t, int64(30), *decoded.Framerate)
	assert.Equal(t, int64(90000), *decoded.Timescale)
	assert.Equal(t, int64(5000000), *decoded.Bitrate)
	assert.Equal(t, int64(1920), *decoded.Width)
	assert.Equal(t, int64(1080), *decoded.Height)
	assert.Equal(t, int64(48000), *decoded.SampleRate)
	assert.Equal(t, "stereo", decoded.ChannelConfig)
	assert.Equal(t, int64(1920), *decoded.DisplayWidth)
	assert.Equal(t, int64(1080), *decoded.DisplayHeight)
	assert.Equal(t, "en", decoded.Language)
}

func TestTrackValidate_EventTimeline(t *testing.T) {
	tests := map[string]struct {
		track        Track
		errorMessage string
	}{
		"eventtimeline missing eventType": {
			track: Track{
				Name:      "events",
				Packaging: PackagingEventTimeline,
				IsLive:    new(true),
			},
			errorMessage: "eventType is required for eventtimeline tracks",
		},
		"mediatimeline eventType must not be set": {
			track: Track{
				Name:      "timeline",
				Packaging: PackagingMediaTimeline,
				EventType: "invalid",
				IsLive:    new(true),
				Depends:   []Dependency{{TrackName: "video"}},
			},
			errorMessage: "eventType must only be set for eventtimeline tracks",
		},
		"loc eventType must not be set": {
			track: Track{
				Name:      "video",
				Packaging: PackagingLOC,
				EventType: "invalid",
				IsLive:    new(true),
			},
			errorMessage: "eventType must only be set for eventtimeline tracks",
		},
		"other packaging eventType must not be set": {
			track: Track{
				Name:      "video",
				Packaging: PackagingCMAF,
				EventType: "invalid",
				IsLive:    new(true),
			},
			errorMessage: "eventType must only be set for eventtimeline tracks",
		},
		"track missing name": {
			track: Track{
				Packaging: PackagingLOC,
				IsLive:    new(true),
			},
			errorMessage: "name is required",
		},
		"track missing packaging": {
			track: Track{
				Name:   "video",
				IsLive: new(true),
			},
			errorMessage: "packaging is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			problems := tt.track.validate(trackListTracks)
			require.NotEmpty(t, problems)
			assert.Contains(t, problems[0], tt.errorMessage)
		})
	}
}

func TestTrackValidate_EventTimelineNeedsNoDependsOrMimeType(t *testing.T) {
	track := Track{Name: "events", Packaging: PackagingEventTimeline, EventType: "com.example.score", IsLive: new(true)}
	assert.Empty(t, track.validate(trackListTracks))
}

func TestCatalogMarshalJSON_AllFields(t *testing.T) {
	generatedAt := int64(9999)
	catalog := Catalog{
		Version:     CurrentVersion,
		GeneratedAt: &generatedAt,
		IsComplete:  true,
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
		}},
	}

	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"generatedAt":9999`)
	assert.Contains(t, string(data), `"isComplete":true`)
	assert.Contains(t, string(data), `"version":"draft-02"`)
}

func TestCatalogUnmarshalJSON_IsComplete(t *testing.T) {
	input := `{"version": "draft-02","isComplete":true}`
	catalog, err := ParseCatalogString(input)
	require.NoError(t, err)
	assert.True(t, catalog.IsComplete)
}

func TestTrack_applyOverrides_AllFields(t *testing.T) {
	base := Track{
		Name:      "video",
		Packaging: PackagingLOC,
		IsLive:    new(true),
	}

	override := Track{
		presentFields: map[string]struct{}{
			"namespace": {}, "name": {}, "packaging": {}, "eventType": {},
			"role": {}, "isLive": {}, "targetLatency": {}, "label": {},
			"renderGroup": {}, "altGroup": {}, "initRef": {}, "depends": {},
			"temporalId": {}, "spatialId": {}, "codec": {}, "mimeType": {},
			"framerate": {}, "timescale": {}, "bitrate": {}, "width": {},
			"height": {}, "samplerate": {}, "channelConfig": {}, "displayWidth": {},
			"displayHeight": {}, "lang": {}, "trackDuration": {},
		},
		Namespace:     "live/new",
		Name:          "audio",
		Packaging:     PackagingCMAF,
		EventType:     "scene",
		Role:          RoleAudio,
		IsLive:        new(false),
		TargetLatency: new(int64(200)),
		Label:         "SD",
		RenderGroup:   new(int64(5)),
		AltGroup:      new(int64(3)),
		InitRef:       "init1",
		Depends:       []Dependency{{TrackName: "sub"}},
		TemporalID:    new(int64(2)),
		SpatialID:     new(int64(0)),
		Codec:         "opus",
		MimeType:      "audio/opus",
		Framerate:     new(int64(60)),
		Timescale:     new(int64(48000)),
		Bitrate:       new(int64(128000)),
		Width:         new(int64(640)),
		Height:        new(int64(480)),
		SampleRate:    new(int64(44100)),
		ChannelConfig: "mono",
		DisplayWidth:  new(int64(640)),
		DisplayHeight: new(int64(480)),
		Language:      "ja",
		TrackDuration: new(int64(60000)),
	}

	base.applyOverrides(override)

	assert.Equal(t, "live/new", base.Namespace)
	assert.Equal(t, "audio", base.Name)
	assert.Equal(t, PackagingCMAF, base.Packaging)
	assert.Equal(t, "scene", base.EventType)
	assert.Equal(t, RoleAudio, base.Role)
	require.NotNil(t, base.IsLive)
	assert.False(t, *base.IsLive)
	assert.Equal(t, int64(200), *base.TargetLatency)
	assert.Equal(t, "SD", base.Label)
	assert.Equal(t, int64(5), *base.RenderGroup)
	assert.Equal(t, int64(3), *base.AltGroup)
	assert.Equal(t, "init1", base.InitRef)
	assert.Equal(t, []Dependency{{TrackName: "sub"}}, base.Depends)
	assert.Equal(t, int64(2), *base.TemporalID)
	assert.Equal(t, int64(0), *base.SpatialID)
	assert.Equal(t, "opus", base.Codec)
	assert.Equal(t, "audio/opus", base.MimeType)
	assert.Equal(t, int64(60), *base.Framerate)
	assert.Equal(t, int64(48000), *base.Timescale)
	assert.Equal(t, int64(128000), *base.Bitrate)
	assert.Equal(t, int64(640), *base.Width)
	assert.Equal(t, int64(480), *base.Height)
	assert.Equal(t, int64(44100), *base.SampleRate)
	assert.Equal(t, "mono", base.ChannelConfig)
	assert.Equal(t, int64(640), *base.DisplayWidth)
	assert.Equal(t, int64(480), *base.DisplayHeight)
	assert.Equal(t, "ja", base.Language)
	assert.Equal(t, int64(60000), *base.TrackDuration)
}

func TestCatalogApplyDelta_AddDuplicateTrack(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		AddTracks:   []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(false)}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot add duplicate track")
}

func TestCatalogApplyDelta_CloneDuplicateTrack(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			{Name: "video-copy", Packaging: PackagingLOC, IsLive: new(true)},
		},
	}
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		CloneTracks: []TrackClone{{
			Name:       "video-copy",
			ParentName: "video",
		}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot clone into duplicate track")
}

func TestCatalogApplyDelta_CloneMissingName(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		CloneTracks: []TrackClone{{
			Track:      Track{},
			ParentName: "video",
		}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestCatalogApplyDelta_InvalidBaseCatalog(t *testing.T) {
	base := Catalog{}
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		AddTracks:   []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog version is required")
}

func TestCatalogApplyDelta_InvalidDelta(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}
	delta := CatalogDelta{}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog must contain")
}

func TestDecodeOrderedObject_InvalidJSON(t *testing.T) {
	tests := map[string]struct {
		input        string
		errorMessage string
	}{
		"not an object": {
			input:        `[1,2,3]`,
			errorMessage: "expected JSON object",
		},
		"empty input": {
			input:        ``,
			errorMessage: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := decodeOrderedObject([]byte(tt.input))
			require.Error(t, err)
			if tt.errorMessage != "" {
				assert.Contains(t, err.Error(), tt.errorMessage)
			}
		})
	}
}

func TestPackaging_IsKnown_AllVariants(t *testing.T) {
	tests := map[string]struct {
		packaging Packaging
		known     bool
	}{
		"event timeline": {
			packaging: PackagingEventTimeline,
			known:     true,
		},
		"cmaf": {
			packaging: PackagingCMAF,
			known:     true,
		},
		"legacy": {
			packaging: PackagingLegacy,
			known:     true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.known, tt.packaging.IsKnown())
		})
	}
}

func TestRole_IsKnown_AllVariants(t *testing.T) {
	tests := map[string]struct {
		role  Role
		known bool
	}{
		"audiodescription": {
			role:  RoleAudioDescription,
			known: true,
		},
		"caption": {
			role:  RoleCaption,
			known: true,
		},
		"subtitle": {
			role:  RoleSubtitle,
			known: true,
		},
		"signlanguage": {
			role:  RoleSignLanguage,
			known: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.known, tt.role.IsKnown())
		})
	}
}

func TestCatalogMarshalJSON_EmptyCatalog(t *testing.T) {
	catalog := Catalog{}
	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	assert.Equal(t, `{"tracks":[]}`, string(data))
}

func TestTrack_hasField_UnknownField(t *testing.T) {
	track := Track{}
	assert.False(t, track.hasField("unknownField"))
}

func TestTrack_hasField_MateriallySet(t *testing.T) {
	track := Track{
		Namespace:     "live",
		Name:          "video",
		Packaging:     PackagingLOC,
		EventType:     "test",
		Role:          RoleVideo,
		IsLive:        new(true),
		TargetLatency: new(int64(100)),
		Label:         "HD",
		RenderGroup:   new(int64(1)),
		AltGroup:      new(int64(1)),
		InitRef:       "init1",
		Depends:       []Dependency{{TrackName: "a"}},
		TemporalID:    new(int64(0)),
		SpatialID:     new(int64(0)),
		Codec:         "av01",
		MimeType:      "video/mp4",
		Framerate:     new(int64(30)),
		Timescale:     new(int64(90000)),
		Bitrate:       new(int64(5000)),
		Width:         new(int64(1920)),
		Height:        new(int64(1080)),
		SampleRate:    new(int64(48000)),
		ChannelConfig: "stereo",
		DisplayWidth:  new(int64(1920)),
		DisplayHeight: new(int64(1080)),
		Language:      "en",
		TrackDuration: new(int64(60000)),
	}

	fields := []string{
		"namespace", "name", "packaging", "eventType", "role", "isLive",
		"targetLatency", "label", "renderGroup", "altGroup", "initRef",
		"depends", "temporalId", "spatialId", "codec", "mimeType",
		"framerate", "timescale", "bitrate", "width", "height",
		"samplerate", "channelConfig", "displayWidth", "displayHeight",
		"lang", "trackDuration",
	}

	for _, field := range fields {
		assert.True(t, track.hasField(field), "hasField(%q) should be true", field)
	}
}

func TestTrackDuration_VODRoundTrip(t *testing.T) {
	input := `{
		"version": "draft-02",
		"tracks": [{
			"name": "video",
			"packaging": "loc",
			"isLive": false,
			"trackDuration": 120000
		}]
	}`

	catalog, err := ParseCatalogString(input)
	require.NoError(t, err)
	require.Len(t, catalog.Tracks, 1)
	require.NotNil(t, catalog.Tracks[0].TrackDuration)
	assert.Equal(t, int64(120000), *catalog.Tracks[0].TrackDuration)

	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"trackDuration":120000`)
}

func TestCatalogDelta_Clone(t *testing.T) {
	generatedAt := int64(5000)
	delta := CatalogDelta{
		DefaultNamespace: "live/demo",
		GeneratedAt:      &generatedAt,
		AddTracks:        []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
		RemoveTracks:     []TrackRef{{Name: "old", Namespace: "ns"}},
		CloneTracks:      []TrackClone{{Track: Track{Name: "video-720"}, ParentName: "video-1080"}},
		UpdateTracks:     []TrackUpdate{{Track: Track{Bitrate: new(int64(1))}, ParentName: "video"}},
		ExtraFields:      map[string]json.RawMessage{"ext": json.RawMessage(`1`)},
		ops:              []deltaOp{{kind: deltaOperationAdd, n: 1}, {kind: deltaOperationRemove, n: 1}, {kind: deltaOperationClone, n: 1}},
	}

	clone := delta.Clone()
	assert.Equal(t, delta.DefaultNamespace, clone.DefaultNamespace)
	require.NotNil(t, clone.GeneratedAt)
	assert.Equal(t, generatedAt, *clone.GeneratedAt)
	require.Len(t, clone.AddTracks, 1)
	assert.Equal(t, "video", clone.AddTracks[0].Name)
	require.Len(t, clone.RemoveTracks, 1)
	assert.Equal(t, "old", clone.RemoveTracks[0].Name)
	require.Len(t, clone.CloneTracks, 1)
	assert.Equal(t, "video-720", clone.CloneTracks[0].Name)
	require.Len(t, clone.UpdateTracks, 1)
	assert.Equal(t, "video", clone.UpdateTracks[0].ParentName)
	assert.Contains(t, clone.ExtraFields, "ext")
	assert.Equal(t, []deltaOp{{kind: deltaOperationAdd, n: 1}, {kind: deltaOperationRemove, n: 1}, {kind: deltaOperationClone, n: 1}}, clone.ops)

	// Mutating clone should not affect original.
	clone.AddTracks[0].Name = "mutated"
	assert.Equal(t, "video", delta.AddTracks[0].Name)
	*clone.GeneratedAt = 1
	assert.Equal(t, generatedAt, *delta.GeneratedAt)
	*clone.UpdateTracks[0].Bitrate = 2
	assert.Equal(t, int64(1), *delta.UpdateTracks[0].Bitrate)

	clone.ops[0].kind = deltaOperationRemove
	assert.Equal(t, []deltaOp{{kind: deltaOperationAdd, n: 1}, {kind: deltaOperationRemove, n: 1}, {kind: deltaOperationClone, n: 1}}, delta.ops)
}

func TestCatalogDelta_MarshalJSON_RoundTrip(t *testing.T) {
	generatedAt := int64(42)
	delta := CatalogDelta{
		GeneratedAt: &generatedAt,
		AddTracks:   []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
		RemoveTracks: []TrackRef{{
			Namespace: "live",
			Name:      "old",
		}},
		CloneTracks: []TrackClone{{
			Name: "video-720", Width: new(int64(1280)),
			ParentName: "video-1080",
		}},
		ExtraFields: map[string]json.RawMessage{"custom": json.RawMessage(`true`)},
	}

	data, err := json.Marshal(delta)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"deltaUpdate"`)
	assert.Contains(t, string(data), `"generatedAt":42`)
	assert.NotContains(t, string(data), `"isComplete"`)
	assert.Contains(t, string(data), `"op":"add"`)
	assert.Contains(t, string(data), `"op":"remove"`)
	assert.Contains(t, string(data), `"op":"clone"`)
	assert.Contains(t, string(data), `"custom":true`)

	var decoded CatalogDelta
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.GeneratedAt)
	assert.Equal(t, int64(42), *decoded.GeneratedAt)
	require.Len(t, decoded.AddTracks, 1)
	assert.Equal(t, "video", decoded.AddTracks[0].Name)
	require.Len(t, decoded.RemoveTracks, 1)
	assert.Equal(t, "old", decoded.RemoveTracks[0].Name)
	require.Len(t, decoded.CloneTracks, 1)
	assert.Equal(t, "video-720", decoded.CloneTracks[0].Name)
	assert.Equal(t, "video-1080", decoded.CloneTracks[0].ParentName)
}

func TestCatalogDelta_MarshalJSON_EmptyDelta(t *testing.T) {
	delta := CatalogDelta{}
	data, err := json.Marshal(delta)
	require.NoError(t, err)
	assert.Equal(t, `{"deltaUpdate":[]}`, string(data))
}

func TestTrackRef_MarshalJSON_RoundTrip(t *testing.T) {
	ref := TrackRef{
		Namespace: "live/demo",
		Name:      "video",
	}

	data, err := json.Marshal(ref)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"namespace":"live/demo"`)
	assert.Contains(t, string(data), `"name":"video"`)

	var decoded TrackRef
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "live/demo", decoded.Namespace)
	assert.Equal(t, "video", decoded.Name)
}

func TestTrackRef_Clone(t *testing.T) {
	ref := TrackRef{
		Namespace: "live",
		Name:      "video",
		ExtraFields: map[string]json.RawMessage{
			"x":      json.RawMessage(`[1, 2, 3]`),
			"nilval": nil,
		},
	}

	clone := ref.Clone()
	assert.Equal(t, ref.Namespace, clone.Namespace)
	assert.Equal(t, ref.Name, clone.Name)
	assert.Contains(t, clone.ExtraFields, "x")
	assert.Contains(t, clone.ExtraFields, "nilval")
	assert.Nil(t, clone.ExtraFields["nilval"])

	// Test byte slice deep copy: mutating clone's underlying slice should not affect original
	clone.ExtraFields["x"][1] = '9'
	assert.Equal(t, byte('1'), ref.ExtraFields["x"][1], "original byte slice should not be mutated")

	// Test map shallow copy: re-assigning map entry should not affect original
	clone.ExtraFields["y"] = json.RawMessage(`[4, 5, 6]`)
	assert.NotContains(t, ref.ExtraFields, "y")
}

func TestTrackRef_ID(t *testing.T) {
	tests := map[string]struct {
		ref              TrackRef
		defaultNamespace string
		expected         TrackID
	}{
		"explicit namespace": {
			ref:              TrackRef{Namespace: "live", Name: "video"},
			defaultNamespace: "other",
			expected:         TrackID{Namespace: "live", Name: "video"},
		},
		"inherits default": {
			ref:              TrackRef{Name: "audio"},
			defaultNamespace: "live/demo",
			expected:         TrackID{Namespace: "live/demo", Name: "audio"},
		},
		"sentinel when both empty": {
			ref:              TrackRef{Name: "data"},
			defaultNamespace: "",
			expected:         TrackID{Namespace: inheritedNamespaceSentinel, Name: "data"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.ID(tt.defaultNamespace))
		})
	}
}

func TestTrackRef_effectiveNamespace(t *testing.T) {
	tests := map[string]struct {
		ref              TrackRef
		defaultNamespace string
		expected         string
	}{
		"explicit namespace": {
			ref:              TrackRef{Namespace: "live"},
			defaultNamespace: "other",
			expected:         "live",
		},
		"inherits default": {
			ref:              TrackRef{},
			defaultNamespace: "live/demo",
			expected:         "live/demo",
		},
		"sentinel when both empty": {
			ref:              TrackRef{},
			defaultNamespace: "",
			expected:         inheritedNamespaceSentinel,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.effectiveNamespace(tt.defaultNamespace))
		})
	}
}

func TestTrackClone_MarshalJSON_RoundTrip(t *testing.T) {
	clone := TrackClone{
		Name:       "video-720",
		Width:      new(int64(1280)),
		Height:     new(int64(720)),
		Bitrate:    new(int64(3000000)),
		ParentName: "video-1080",
	}

	data, err := json.Marshal(clone)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"parentName":"video-1080"`)
	assert.Contains(t, string(data), `"name":"video-720"`)

	var decoded TrackClone
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "video-720", decoded.Name)
	assert.Equal(t, "video-1080", decoded.ParentName)
	require.NotNil(t, decoded.Width)
	assert.Equal(t, int64(1280), *decoded.Width)
}

func TestTrackClone_MarshalJSON(t *testing.T) {
	t.Run("with parent name", func(t *testing.T) {
		clone := TrackClone{
			Name:       "video-720",
			ParentName: "video-1080",
		}
		data, err := clone.MarshalJSON()
		require.NoError(t, err)
		assert.JSONEq(t, `{"name":"video-720","parentName":"video-1080"}`, string(data))
	})

	t.Run("without parent name", func(t *testing.T) {
		clone := TrackClone{
			Name: "video-720",
		}
		data, err := clone.MarshalJSON()
		require.NoError(t, err)
		assert.JSONEq(t, `{"name":"video-720"}`, string(data))
	})
}

func TestTrackClone_Clone(t *testing.T) {
	original := TrackClone{
		Name:  "video-720",
		Codec: "av01",
		ExtraFields: map[string]json.RawMessage{
			"x": json.RawMessage(`[1, 2, 3]`),
		},
		ParentName: "video-1080",
	}

	clone := original.Clone()
	assert.Equal(t, "video-720", clone.Name)
	assert.Equal(t, "video-1080", clone.ParentName)
	assert.Equal(t, "av01", clone.Codec)
	assert.Contains(t, clone.ExtraFields, "x")

	// Test byte slice deep copy
	clone.ExtraFields["x"][1] = '9'
	assert.Equal(t, byte('1'), original.ExtraFields["x"][1], "original byte slice should not be mutated")

	clone.Name = "mutated"
	assert.Equal(t, "video-720", original.Name)
}

func TestTrackClone_Validate(t *testing.T) {
	tests := map[string]struct {
		clone          TrackClone
		expectedErrors []string
	}{
		"valid": {
			clone:          TrackClone{Track: Track{Name: "video-720"}, ParentName: "video-1080"},
			expectedErrors: nil,
		},
		"missing name": {
			clone:          TrackClone{Track: Track{}, ParentName: "parent"},
			expectedErrors: []string{"test: name is required"},
		},
		"missing parentName": {
			clone:          TrackClone{Track: Track{Name: "video"}},
			expectedErrors: []string{"test: parentName is required for clone tracks"},
		},
		"missing both": {
			clone:          TrackClone{Track: Track{}},
			expectedErrors: []string{"test: name is required", "test: parentName is required for clone tracks"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			problems := tt.clone.Validate("test")
			if len(tt.expectedErrors) == 0 {
				require.Empty(t, problems)
			} else {
				require.Equal(t, tt.expectedErrors, problems)
			}
		})
	}
}

func TestCatalogDeltaValidate_Valid(t *testing.T) {
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		AddTracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
		},
	}
	require.NoError(t, delta.Validate())
}

func TestCatalogDeltaValidate_Errors(t *testing.T) {
	tests := map[string]struct {
		delta        CatalogDelta
		errorMessage string
	}{
		"missing operations": {
			delta:        CatalogDelta{},
			errorMessage: "delta catalog must contain at least one add, remove, clone, or update track",
		},
		"add track invalid": {
			delta: CatalogDelta{
				GeneratedAt: new(int64(1)),
				AddTracks:   []Track{{Packaging: PackagingLOC}}, // missing name
			},
			errorMessage: "add[0]: name is required",
		},
		"remove track invalid": {
			delta: CatalogDelta{
				GeneratedAt:  new(int64(1)),
				RemoveTracks: []TrackRef{{Namespace: "ns"}}, // missing name
			},
			errorMessage: "remove[0]: name is required",
		},
		"clone track invalid": {
			delta: CatalogDelta{
				GeneratedAt: new(int64(1)),
				CloneTracks: []TrackClone{{Name: "video"}}, // missing parentName
			},
			errorMessage: "clone[0]: parentName is required for clone tracks",
		},
		"multiple invalid": {
			delta: CatalogDelta{
				GeneratedAt:  new(int64(1)),
				AddTracks:    []Track{{Packaging: PackagingLOC}}, // missing name
				RemoveTracks: []TrackRef{{Namespace: "ns"}},      // missing name
			},
			errorMessage: "add[0]: name is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.delta.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMessage)
		})
	}
}

func TestCatalogDeltaValidate_EmptyDelta(t *testing.T) {
	delta := CatalogDelta{}
	err := delta.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog must contain")
}

func TestCatalogDelta_UnmarshalJSON(t *testing.T) {
	tests := map[string]struct {
		input       string
		wantErr     bool
		errContains string
		check       func(*testing.T, CatalogDelta)
	}{
		"valid base delta": {
			input:   `{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[{"name": "video", "packaging": "loc", "isLive": true}]}]}`,
			wantErr: false,
			check: func(t *testing.T, d CatalogDelta) {
				require.Len(t, d.AddTracks, 1)
				assert.Equal(t, "video", d.AddTracks[0].Name)
				assert.Empty(t, d.ExtraFields)
			},
		},
		"valid delta with extra fields": {
			input:   `{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[{"name": "video", "packaging": "loc", "isLive": true}]}], "custom_field": "custom_value", "another_field": 123}`,
			wantErr: false,
			check: func(t *testing.T, d CatalogDelta) {
				require.Len(t, d.AddTracks, 1)
				assert.Len(t, d.ExtraFields, 2)
				assert.Contains(t, d.ExtraFields, "custom_field")
				assert.Contains(t, d.ExtraFields, "another_field")
				assert.Equal(t, string(d.ExtraFields["custom_field"]), `"custom_value"`)
				assert.Equal(t, string(d.ExtraFields["another_field"]), "123")
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var delta CatalogDelta
			err := json.Unmarshal([]byte(tc.input), &delta)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t, delta)
				}
			}
		})
	}
}

func TestCatalogDelta_UnmarshalJSON_MissingDeltaUpdate(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"addTracks":[{"name":"video","packaging":"loc","isLive":true}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog must include a deltaUpdate array")
}

func TestCatalogDelta_UnmarshalJSON_DeltaUpdateFalse(t *testing.T) {
	_, err := ParseCatalogDelta([]byte(`{"deltaUpdate":false}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deltaUpdate must be an array of operation objects")
}

func TestCatalogDelta_UnmarshalJSON_CloneTracks(t *testing.T) {
	input := `{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "clone", "tracks": [{"name": "video-720", "parentName": "video-1080", "width": 1280}]}
		]
	}`

	delta, err := ParseCatalogDeltaString(input)
	require.NoError(t, err)
	require.Len(t, delta.CloneTracks, 1)
	assert.Equal(t, "video-720", delta.CloneTracks[0].Name)
	assert.Equal(t, "video-1080", delta.CloneTracks[0].ParentName)
	require.NotNil(t, delta.CloneTracks[0].Width)
	assert.Equal(t, int64(1280), *delta.CloneTracks[0].Width)
}

func TestCatalogDelta_UnmarshalJSON_ExtraFields(t *testing.T) {
	input := `{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [{"name": "video", "packaging": "loc", "isLive": true}]}
		],
		"com.example.ext": 42
	}`

	delta, err := ParseCatalogDeltaString(input)
	require.NoError(t, err)
	assert.Contains(t, delta.ExtraFields, "com.example.ext")
}

func TestTrackRef_MarshalJSON_ExtraFields(t *testing.T) {
	ref := TrackRef{
		Name: "video",
		ExtraFields: map[string]json.RawMessage{
			"customField": json.RawMessage(`"customValue"`),
		},
	}

	data, err := json.Marshal(ref)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "video", decoded["name"])
	assert.Equal(t, "customValue", decoded["customField"])
}

func TestTrackRef_MarshalJSON_NameOnly(t *testing.T) {
	ref := TrackRef{Name: "video"}

	data, err := json.Marshal(ref)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "video", decoded["name"])
	_, hasNS := decoded["namespace"]
	assert.False(t, hasNS)
}

// --- cloneBoolPtr / cloneRawMessage nil branch coverage ---

func TestTrack_Clone_NilBoolPtr(t *testing.T) {
	track := Track{Name: "video", Packaging: PackagingLOC} // IsLive nil
	clone := track.Clone()
	assert.Nil(t, clone.IsLive)
}

func TestTrackRef_Clone_NilExtraFields(t *testing.T) {
	ref := TrackRef{Name: "video"} // ExtraFields nil
	clone := ref.Clone()
	assert.NotNil(t, clone.ExtraFields)
	assert.Empty(t, clone.ExtraFields)
}

// --- CatalogDelta.Clone nil-slice branches ---

func TestCatalogDelta_Clone_NilSlices(t *testing.T) {
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		AddTracks:   nil, RemoveTracks: nil, CloneTracks: nil,
	}
	clone := delta.Clone()
	assert.Nil(t, clone.AddTracks)
	assert.Nil(t, clone.RemoveTracks)
	assert.Nil(t, clone.CloneTracks)
}

// --- Track.UnmarshalJSON error paths ---

func TestTrackUnmarshalJSON_InvalidJSON(t *testing.T) {
	var track Track
	err := json.Unmarshal([]byte(`"not-an-object"`), &track)
	require.Error(t, err)
}

func TestTrack_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]string{
		"namespace":     `{"namespace": 123}`,
		"name":          `{"name": 123}`,
		"packaging":     `{"packaging": 123}`,
		"eventType":     `{"eventType": 123}`,
		"role":          `{"role": 123}`,
		"isLive":        `{"isLive": "notbool"}`,
		"targetLatency": `{"targetLatency": "notnum"}`,
		"label":         `{"label": 123}`,
		"renderGroup":   `{"renderGroup": "notnum"}`,
		"altGroup":      `{"altGroup": "notnum"}`,
		"initRef":       `{"initRef": 123}`,
		"depends":       `{"depends": "notarray"}`,
		"temporalId":    `{"temporalId": "notnum"}`,
		"spatialId":     `{"spatialId": "notnum"}`,
		"codec":         `{"codec": 123}`,
		"mimeType":      `{"mimeType": 123}`,
		"framerate":     `{"framerate": "notnum"}`,
		"timescale":     `{"timescale": "notnum"}`,
		"bitrate":       `{"bitrate": "notnum"}`,
		"width":         `{"width": "notnum"}`,
		"height":        `{"height": "notnum"}`,
		"samplerate":    `{"samplerate": "notnum"}`,
		"channelConfig": `{"channelConfig": 123}`,
		"displayWidth":  `{"displayWidth": "notnum"}`,
		"displayHeight": `{"displayHeight": "notnum"}`,
		"lang":          `{"lang": 123}`,
		"trackDuration": `{"trackDuration": "notnum"}`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			var track Track
			err := json.Unmarshal([]byte(input), &track)
			require.Error(t, err, "field %q with invalid type should fail", name)
		})
	}
}

// --- Catalog.UnmarshalJSON error paths ---

func TestCatalogUnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"bad version":     {`{"version": 2}`},
		"bad generatedAt": {`{"version": "draft-02", "generatedAt": "not-a-number"}`},
		"bad isComplete":  {`{"version": "draft-02", "isComplete": "not-a-bool"}`},
		"bad tracks":      {`{"version": "draft-02", "tracks": "not-an-array"}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalogString(tt.input)
			require.Error(t, err)
		})
	}
}

// --- decodeOrderedObject error paths ---

func TestDecodeOrderedObject_TruncatedAfterKey(t *testing.T) {
	// Decode fails when the value is missing after the colon.
	_, err := decodeOrderedObject([]byte(`{"key":`))
	require.Error(t, err)
}

func TestDecodeOrderedObject_TruncatedBeforeClosingBrace(t *testing.T) {
	// Token() for closing brace fails when `}` is absent.
	_, err := decodeOrderedObject([]byte(`{"key": 1`))
	require.Error(t, err)
}

func TestDecodeOrderedObject_TrailingGarbage(t *testing.T) {
	// Non-JSON trailing bytes cause a non-EOF error on the second token read.
	_, err := decodeOrderedObject([]byte(`{"version": "draft-02"}abc`))
	require.Error(t, err)
}

// --- CatalogDelta.UnmarshalJSON error paths ---

func TestCatalogDelta_UnmarshalJSON_IndependentFields(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"version field": {`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[]}], "version": "draft-02"}`},
		"tracks field":  {`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[]}], "tracks": []}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalogDeltaString(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is not allowed in a delta catalog")
		})
	}
}

func TestCatalogDelta_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"bad deltaUpdate":  {`{"deltaUpdate": "not-an-array"}`},
		"bad generatedAt":  {`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[]}], "generatedAt": "not-a-number"}`},
		"bad isComplete":   {`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":[]}], "isComplete": "not-a-bool"}`},
		"bad addTracks":    {`{"generatedAt": 1, "deltaUpdate": [{"op":"add","tracks":"not-an-array"}]}`},
		"bad removeTracks": {`{"generatedAt": 1, "deltaUpdate": [{"op":"remove","tracks":"not-an-array"}]}`},
		"bad cloneTracks":  {`{"generatedAt": 1, "deltaUpdate": [{"op":"clone","tracks":"not-an-array"}]}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalogDeltaString(tt.input)
			require.Error(t, err)
		})
	}
}

// --- TrackRef.UnmarshalJSON error paths ---

func TestTrackRef_UnmarshalJSON(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected TrackRef
	}{
		"empty object": {
			input: `{}`,
			expected: TrackRef{
				ExtraFields: map[string]json.RawMessage{},
			},
		},
		"name only": {
			input: `{"name":"video"}`,
			expected: TrackRef{
				Name:        "video",
				ExtraFields: map[string]json.RawMessage{},
			},
		},
		"namespace and name": {
			input: `{"namespace":"live/demo","name":"video"}`,
			expected: TrackRef{
				Namespace:   "live/demo",
				Name:        "video",
				ExtraFields: map[string]json.RawMessage{},
			},
		},
		"with extra fields": {
			input: `{"name":"video","extra":"value"}`,
			expected: TrackRef{
				Name: "video",
				ExtraFields: map[string]json.RawMessage{
					"extra": json.RawMessage(`"value"`),
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var ref TrackRef
			err := json.Unmarshal([]byte(tt.input), &ref)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ref)
		})
	}
}

func TestTrackRef_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"invalid JSON":  {`"not-an-object"`},
		"bad namespace": {`{"namespace": 123}`},
		"bad name":      {`{"name": 123}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var ref TrackRef
			err := json.Unmarshal([]byte(tt.input), &ref)
			require.Error(t, err)
		})
	}
}

func TestTrackClone_UnmarshalJSON_Success(t *testing.T) {
	w := int64(1280)
	tests := map[string]struct {
		input    string
		expected TrackClone
	}{
		"basic clone": {
			input: `{"name":"video-720","parentName":"video-1080"}`,
			expected: TrackClone{
				Track:      Track{Name: "video-720"},
				ParentName: "video-1080",
			},
		},
		"with extra fields": {
			input: `{"name":"video-720","parentName":"video-1080","width":1280}`,
			expected: TrackClone{
				Track: Track{
					Name:  "video-720",
					Width: &w,
				},
				ParentName: "video-1080",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var clone TrackClone
			err := json.Unmarshal([]byte(tt.input), &clone)
			require.NoError(t, err)

			assert.Equal(t, tt.expected.Name, clone.Name)
			assert.Equal(t, tt.expected.ParentName, clone.ParentName)
			if tt.expected.Width != nil {
				require.NotNil(t, clone.Width)
				assert.Equal(t, *tt.expected.Width, *clone.Width)
			}
		})
	}
}

// --- TrackClone.UnmarshalJSON error paths ---

func TestTrackClone_UnmarshalJSON_FieldErrors(t *testing.T) {
	tests := map[string]struct {
		input string
	}{
		"invalid JSON":    {`"not-an-object"`},
		"bad parentName":  {`{"name": "v", "parentName": 123}`},
		"bad track field": {`{"name": 123, "parentName": "parent"}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var clone TrackClone
			err := json.Unmarshal([]byte(tt.input), &clone)
			require.Error(t, err)
		})
	}
}

// --- cloneTrack unknown parent ---

func TestCatalogApplyDelta_UnknownParentTrack(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}
	delta := CatalogDelta{
		GeneratedAt: new(int64(1)),
		CloneTracks: []TrackClone{{
			Name:       "copy",
			ParentName: "nonexistent",
		}},
	}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot clone unknown parent")
}

// --- RegisterTrack replace existing track ---

func TestBroadcast_RegisterTrack_ReplacesExisting(t *testing.T) {
	b, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	handler1 := &fakeTrackHandler{}
	require.NoError(t, b.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, handler1))

	// Register same track name again — hits replaced=true path.
	handler2 := &fakeTrackHandler{}
	err = b.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(false)}, handler2)
	require.NoError(t, err)

	catalog := b.Catalog()
	require.Len(t, catalog.Tracks, 1)
	require.NotNil(t, catalog.Tracks[0].IsLive)
	assert.False(t, *catalog.Tracks[0].IsLive)
}

// --- staleTrackNamesLocked continue branch ---

func TestBroadcast_SetCatalog_KeepsActiveRemovesStale(t *testing.T) {
	b, err := NewBroadcast(Catalog{Version: CurrentVersion})
	require.NoError(t, err)

	require.NoError(t, b.RegisterTrack(Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))
	require.NoError(t, b.RegisterTrack(Track{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)}, &fakeTrackHandler{}))

	// New catalog keeps "video", drops "audio" → staleTrackNamesLocked hits continue for "video", appends "audio".
	newCatalog := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
	}
	require.NoError(t, b.SetCatalog(newCatalog))

	catalog := b.Catalog()
	assert.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "video", catalog.Tracks[0].Name)
}

func TestParseCatalogDeltaString(t *testing.T) {
	input := `{
		"generatedAt": 1, "deltaUpdate": [
			{"op": "add", "tracks": [
				{"name": "video", "packaging": "loc", "isLive": true}
			]}
		]
	}`

	delta, err := ParseCatalogDeltaString(input)
	require.NoError(t, err)
	require.Len(t, delta.AddTracks, 1)
	assert.Equal(t, "video", delta.AddTracks[0].Name)

	_, err = ParseCatalogDeltaString(`{"deltaUpdate": "invalid_type"}`)
	require.Error(t, err)
}

func TestTrackRef_Validate(t *testing.T) {
	tests := map[string]struct {
		ref      TrackRef
		path     string
		expected []string
	}{
		"valid": {
			ref: TrackRef{
				Namespace: "foo",
				Name:      "bar",
			},
			path:     "test",
			expected: nil,
		},
		"empty name": {
			ref: TrackRef{
				Namespace: "foo",
				Name:      "",
			},
			path:     "test",
			expected: []string{"test: name is required"},
		},
		"extra fields": {
			ref: TrackRef{
				Namespace: "foo",
				Name:      "bar",
				ExtraFields: map[string]json.RawMessage{
					"unknown": json.RawMessage(`"value"`),
				},
			},
			path:     "test",
			expected: []string{"test: remove track entries may contain only name and optional namespace"},
		},
		"empty name and extra fields": {
			ref: TrackRef{
				Namespace: "foo",
				Name:      "",
				ExtraFields: map[string]json.RawMessage{
					"unknown": json.RawMessage(`"value"`),
				},
			},
			path: "test",
			expected: []string{
				"test: name is required",
				"test: remove track entries may contain only name and optional namespace",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			problems := tc.ref.Validate(tc.path)
			assert.Equal(t, tc.expected, problems)
		})
	}
}

// --- draft-01 new field coverage -------------------------------------------

func TestTrackRoundTrip_NewFields(t *testing.T) {
	target := int64(500)
	min := int64(100)
	max := int64(2000)
	track := Track{
		Namespace:        "live/demo",
		Name:             "video",
		Packaging:        PackagingLOC,
		IsLive:           new(true),
		InitRef:          "init1",
		Buffers:          &Buffers{Target: &target, Min: &min, Max: &max},
		AvgBitrate:       new(int64(2500000)),
		MaxGopDuration:   new(int64(1000)),
		MaxGroupDuration: new(int64(2000)),
		ConnectionURI:    "moqt://example.com/live",
		EncryptionScheme: "cenc",
		CipherSuite:      "AES_256_GCM",
		KeyID:            "k1",
		TrackBaseKey:     "base64key",
		AuthInfo:         map[string]json.RawMessage{"bearer": json.RawMessage(`"xyz"`)},
		Accessibility:    []AccessibilityDescriptor{{Scheme: "cea", Value: "608"}},

		PackagingMetadata:    "streamMapping=object",
		AccessibleWindowTime: new(int64(30000)),
		KeyframeSpacing:      new(int64(60)),
	}

	data, err := json.Marshal(track)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"connectionURI":"moqt://example.com/live"`)
	assert.Contains(t, string(data), `"packagingMetadata":"streamMapping=object"`)
	assert.Contains(t, string(data), `"accessibleWindowTime":30000`)
	assert.Contains(t, string(data), `"keyframeSpacing":60`)

	var decoded Track
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, "init1", decoded.InitRef)
	require.NotNil(t, decoded.Buffers)
	require.NotNil(t, decoded.Buffers.Target)
	assert.Equal(t, target, *decoded.Buffers.Target)
	assert.Equal(t, "cenc", decoded.EncryptionScheme)
	assert.Equal(t, "AES_256_GCM", decoded.CipherSuite)
	assert.Equal(t, "k1", decoded.KeyID)
	assert.Equal(t, "base64key", decoded.TrackBaseKey)
	assert.Equal(t, "moqt://example.com/live", decoded.ConnectionURI)
	assert.Equal(t, "streamMapping=object", decoded.PackagingMetadata)
	assert.Equal(t, StreamMappingObject, decoded.StreamMapping())
	require.NotNil(t, decoded.AccessibleWindowTime)
	assert.Equal(t, int64(30000), *decoded.AccessibleWindowTime)
	require.NotNil(t, decoded.KeyframeSpacing)
	assert.Equal(t, int64(60), *decoded.KeyframeSpacing)
	require.Len(t, decoded.Accessibility, 1)
	assert.Equal(t, "cea", decoded.Accessibility[0].Scheme)
	assert.Equal(t, "608", decoded.Accessibility[0].Value)
	assert.Contains(t, decoded.AuthInfo, "bearer")
}

func TestCatalog_NewPackagingAndRoleConstants(t *testing.T) {
	assert.True(t, PackagingMoqLog.IsKnown())
	assert.True(t, PackagingMoqMetrics.IsKnown())
	assert.Equal(t, "moqlog", PackagingMoqLog.String())
	assert.Equal(t, "moqmetrics", PackagingMoqMetrics.String())

	assert.True(t, RoleLog.IsKnown())
	assert.True(t, RoleMetrics.IsKnown())
	assert.Equal(t, "log", RoleLog.String())
	assert.Equal(t, "metrics", RoleMetrics.String())
}

func TestCatalog_PublishTracksAndInitDataList_RoundTrip(t *testing.T) {
	target := int64(100)
	catalog := Catalog{
		Version: CurrentVersion,
		PublishTracks: []Track{{
			Namespace:     "moq-syslog.arpa/logs-v1/r1",
			Name:          "6",
			Packaging:     PackagingMoqLog,
			Role:          RoleLog,
			IsLive:        new(true),
			ConnectionURI: "moqt://example.com/log",
		}},
		InitDataList: []InitDataRef{
			{ID: "init1", Type: "inline", Data: "AAAA"},
		},
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
			InitRef:   "init1",
			Buffers:   &Buffers{Target: &target},
		}},
	}

	data, err := json.Marshal(catalog)
	require.NoError(t, err)

	var decoded Catalog
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded.PublishTracks, 1)
	assert.Equal(t, "6", decoded.PublishTracks[0].Name)
	// initDataList is encoded after tracks (§5.1.7).
	assert.Greater(t, strings.Index(string(data), `"initDataList"`), strings.Index(string(data), `"tracks"`))
	assert.Equal(t, PackagingMoqLog, decoded.PublishTracks[0].Packaging)
	require.Len(t, decoded.InitDataList, 1)
	assert.Equal(t, "init1", decoded.InitDataList[0].ID)
	assert.Equal(t, "inline", decoded.InitDataList[0].Type)
	assert.Equal(t, "AAAA", decoded.InitDataList[0].Data)

	require.NoError(t, decoded.Validate())
}

func TestCatalogValidate_CipherSuiteRequiredForMoQSecureObjects(t *testing.T) {
	tests := map[string]struct {
		scheme  string
		wantErr bool
	}{
		"moq-secure-objects without cipherSuite": {scheme: EncryptionSchemeMoQSecureObjects, wantErr: true},
		"custom scheme without cipherSuite":      {scheme: "com.example.custom-encryption", wantErr: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := Catalog{
				Version: CurrentVersion,
				Tracks: []Track{{
					Name:             "video",
					Packaging:        PackagingLOC,
					IsLive:           new(true),
					EncryptionScheme: tt.scheme,
				}},
			}

			err := catalog.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cipherSuite is required when encryptionScheme is moq-secure-objects")
		})
	}
}

func TestCatalogValidate_EncryptionWithCipherSuiteIsValid(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{{
			Name:             "video",
			Packaging:        PackagingLOC,
			IsLive:           new(true),
			EncryptionScheme: "cenc",
			CipherSuite:      "AES_256_GCM",
		}},
	}

	assert.NoError(t, catalog.Validate())
}

func TestCatalogValidate_DanglingInitRef(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		InitDataList: []InitDataRef{
			{ID: "init1", Type: "inline", Data: "AAAA"},
		},
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
			InitRef:   "nonexistent",
		}},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initRef")
	assert.Contains(t, err.Error(), "does not match any initDataList id")
}

func TestCatalogValidate_ValidInitRef(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		InitDataList: []InitDataRef{
			{ID: "init1", Type: "inline", Data: "AAAA"},
		},
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
			InitRef:   "init1",
		}},
	}

	assert.NoError(t, catalog.Validate())
}

func TestCatalogValidate_DuplicateInitDataListIDs(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		InitDataList: []InitDataRef{
			{ID: "init1", Type: "inline", Data: "AAAA"},
			{ID: "init1", Type: "inline", Data: "BBBB"},
		},
		Tracks: []Track{{
			Name:      "video",
			Packaging: PackagingLOC,
			IsLive:    new(true),
			InitRef:   "init1",
		}},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate init id")
}

func TestCatalogValidate_BadInitDataListType(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		InitDataList: []InitDataRef{
			{ID: "init1", Type: "url", Data: "x"},
		},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `must be "inline", "track-property" or "object-property"`)
}

func TestCatalogValidate_InitDataListMissingID(t *testing.T) {
	catalog := Catalog{
		Version: CurrentVersion,
		InitDataList: []InitDataRef{
			{Type: "inline", Data: "x"},
		},
	}

	err := catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")
}

func TestInitDataRef_RoundTrip(t *testing.T) {
	ref := InitDataRef{
		ID:   "init1",
		Type: "inline",
		Data: "AAAA",
		ExtraFields: map[string]json.RawMessage{
			"custom": json.RawMessage(`42`),
		},
	}

	data, err := json.Marshal(ref)
	require.NoError(t, err)

	var decoded InitDataRef
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "init1", decoded.ID)
	assert.Equal(t, "inline", decoded.Type)
	assert.Equal(t, "AAAA", decoded.Data)
	assert.Contains(t, decoded.ExtraFields, "custom")
}

func TestBuffers_RoundTrip(t *testing.T) {
	target := int64(500)
	min := int64(100)
	max := int64(2000)
	b := Buffers{
		Target: &target,
		Min:    &min,
		Max:    &max,
		ExtraFields: map[string]json.RawMessage{
			"unknown": json.RawMessage(`"x"`),
		},
	}

	data, err := json.Marshal(b)
	require.NoError(t, err)

	var decoded Buffers
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.Target)
	assert.Equal(t, target, *decoded.Target)
	require.NotNil(t, decoded.Min)
	assert.Equal(t, min, *decoded.Min)
	require.NotNil(t, decoded.Max)
	assert.Equal(t, max, *decoded.Max)
	assert.Contains(t, decoded.ExtraFields, "unknown")
}

func TestBuffers_Clone_Nil(t *testing.T) {
	var b *Buffers
	assert.Nil(t, b.Clone())
}

func TestAccessibilityDescriptor_RoundTrip(t *testing.T) {
	desc := AccessibilityDescriptor{
		Scheme: "cea",
		Value:  "708",
		ExtraFields: map[string]json.RawMessage{
			"channel": json.RawMessage(`3`),
		},
	}

	data, err := json.Marshal(desc)
	require.NoError(t, err)

	var decoded AccessibilityDescriptor
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "cea", decoded.Scheme)
	assert.Equal(t, "708", decoded.Value)
	assert.Contains(t, decoded.ExtraFields, "channel")
}

// --- draft-02 validation rules ----------------------------------------------

func TestCatalogValidate_Version(t *testing.T) {
	tests := map[string]struct {
		version  string
		errMatch string
	}{
		"current":        {version: CurrentVersion},
		"missing":        {version: "", errMatch: "catalog version is required"},
		"previous draft": {version: "draft-01", errMatch: `unsupported catalog version "draft-01"`},
		"numeric":        {version: "1", errMatch: "unsupported catalog version"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := Catalog{Version: tt.version}.Validate()
			if tt.errMatch == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestCatalogValidate_RequiredFieldsPerPackagingAndRole(t *testing.T) {
	tests := map[string]struct {
		track   Track
		missing []string
	}{
		"loc audio":            {track: Track{Packaging: PackagingLOC, Role: RoleAudio}, missing: []string{"codec", "bitrate", "samplerate", "channelConfig"}},
		"loc video":            {track: Track{Packaging: PackagingLOC, Role: RoleVideo}, missing: []string{"codec", "bitrate", "framerate"}},
		"loc audiodescription": {track: Track{Packaging: PackagingLOC, Role: RoleAudioDescription}, missing: []string{"codec", "samplerate", "channelConfig", "lang"}},
		"loc signlanguage":     {track: Track{Packaging: PackagingLOC, Role: RoleSignLanguage}, missing: []string{"codec", "framerate"}},
		"loc without role":     {track: Track{Packaging: PackagingLOC}},
		"loc caption":          {track: Track{Packaging: PackagingLOC, Role: RoleCaption}},
		"loc data":             {track: Track{Packaging: PackagingLOC, Role: RoleData}},
		"complete loc audio": {
			track: Track{Packaging: PackagingLOC, Role: RoleAudio, Codec: "opus", Bitrate: new(int64(32000)), SampleRate: new(int64(48000)), ChannelConfig: "2"},
		},
		"catalog reference": {track: Track{Packaging: PackagingCatalog}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			track := tt.track
			track.Name = "track"
			track.IsLive = new(true)

			var want []string
			for _, field := range tt.missing {
				want = append(want, field+" is required for loc tracks with role "+string(track.Role))
			}
			assert.Equal(t, want, track.validate(trackListTracks))
		})
	}
}

func TestTrackValidate_Draft02Rules(t *testing.T) {
	tests := map[string]struct {
		track    Track
		list     trackList
		errMatch string
	}{
		"empty namespace field": {
			track:    Track{Namespace: "live//demo", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			errMatch: "contains an empty field",
		},
		"NUL in name": {
			track:    Track{Name: "vi\x00deo", Packaging: PackagingLOC, IsLive: new(true)},
			errMatch: "contains a NUL character",
		},
		"targetLatency with buffers": {
			track:    Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true), TargetLatency: new(int64(1)), Buffers: &Buffers{}},
			errMatch: "targetLatency and buffers must not both be present",
		},
		"bad packagingMetadata": {
			track:    Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true), PackagingMetadata: "streamMapping=frame"},
			errMatch: "streamMapping",
		},
		"empty depends entry": {
			track:    Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{{}}},
			errMatch: "depends[0]: depends entry must not be an empty track name",
		},
		"mediatimeline with template": {
			track:    Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{DependsOnTrack("v")}, Template: &Template{}},
			errMatch: "mediatimeline tracks must not also carry a template",
		},
		"moqlog in tracks": {
			track:    Track{Name: "6", Packaging: PackagingMoqLog, Role: RoleLog, IsLive: new(true)},
			list:     trackListTracks,
			errMatch: "moqlog tracks must be declared in publishTracks",
		},
		"moqlog without role": {
			track:    Track{Name: "6", Packaging: PackagingMoqLog, IsLive: new(true)},
			list:     trackListPublishTracks,
			errMatch: "moqlog tracks must have role log",
		},
		"moqmetrics binary level name": {
			track:    Track{Name: "\x04", Packaging: PackagingMoqMetrics, Role: RoleMetrics, IsLive: new(true)},
			list:     trackListPublishTracks,
			errMatch: `moqmetrics track name must be a level from "0" to "7"`,
		},
		"moqmetrics level out of range": {
			track:    Track{Name: "8", Packaging: PackagingMoqMetrics, Role: RoleMetrics, IsLive: new(true)},
			list:     trackListPublishTracks,
			errMatch: "moqmetrics track name must be a level",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			problems := tt.track.validate(tt.list)
			require.NotEmpty(t, problems)
			assert.Contains(t, strings.Join(problems, "; "), tt.errMatch)
		})
	}
}

func TestTrackValidate_ExplicitEmptyNamespace(t *testing.T) {
	var track Track
	require.NoError(t, json.Unmarshal([]byte(`{"namespace":"","name":"video","packaging":"loc","isLive":true}`), &track))

	assert.Equal(t, []string{"namespace must not be empty"}, track.validate(trackListTracks))
}

func TestTrackValidate_ParentFieldsOnlyInCloneOrUpdate(t *testing.T) {
	catalog, err := ParseCatalogString(`{"version":"draft-02","tracks":[
		{"name":"video","packaging":"loc","isLive":true,"parentName":"x","parentNamespace":"y"}]}`)
	require.NoError(t, err)

	err = catalog.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parentName must only appear in a clone or update operation")
	assert.Contains(t, err.Error(), "parentNamespace must only appear in a clone or update operation")
}

// TestCatalogValidate_PublishTracksSpecExample uses the catalog of
// draft-ietf-moq-msf-02 §5.6.17, after variable substitution.
func TestCatalogValidate_PublishTracksSpecExample(t *testing.T) {
	catalog, err := ParseCatalogString(`{
		"version": "draft-02",
		"generatedAt": 1746104606044,
		"tracks": [{
			"name": "video", "namespace": "broadcast.example.com/live/stream1", "packaging": "loc",
			"isLive": true, "targetLatency": 2000, "role": "video", "renderGroup": 1,
			"codec": "av01.0.08M.10.0.110.09", "width": 1920, "height": 1080, "framerate": 30, "bitrate": 1500000
		}],
		"publishTracks": [
			{"namespace": "metrics.moq.arpa/metrics-v1/r1", "name": "4", "isLive": true,
			 "packaging": "moqmetrics", "role": "metrics", "authInfo": {"cat": "eyJ"}},
			{"namespace": "moq-syslog.arpa/logs-v1/r1", "name": "6", "isLive": true, "packaging": "moqlog",
			 "role": "log", "connectionURI": "moqt://logs.example.com:4443", "authInfo": {"cat": "eyJ"}}
		]
	}`)
	require.NoError(t, err)
	require.NoError(t, catalog.Validate())
	assert.Equal(t, "moqt://logs.example.com:4443", catalog.PublishTracks[1].ConnectionURI)
	assert.Contains(t, catalog.PublishTracks[0].AuthInfo, "cat")
}

func TestCatalogValidate_Dependencies(t *testing.T) {
	video := Track{Name: "video", Packaging: PackagingLOC, IsLive: new(true), RenderGroup: new(int64(1))}
	timeline := func(deps ...Dependency) Track {
		return Track{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: deps}
	}

	tests := map[string]struct {
		tracks        []Track
		publishTracks []Track
		errMatch      string
	}{
		"track name resolves":   {tracks: []Track{video, timeline(DependsOnTrack("video"))}},
		"render group resolves": {tracks: []Track{video, timeline(DependsOnRenderGroup(1))}},
		"publish track depends on a media track": {
			tracks:        []Track{video},
			publishTracks: []Track{{Name: "events", Packaging: PackagingEventTimeline, EventType: "com.example.e", IsLive: new(true), Depends: []Dependency{DependsOnTrack("video")}}},
		},
		"unknown track name": {
			tracks:   []Track{video, timeline(DependsOnTrack("audio"))},
			errMatch: `tracks[1]: depends[0]: track name "audio" does not match any other track`,
		},
		"self reference": {
			tracks:   []Track{video, timeline(DependsOnTrack("timeline"))},
			errMatch: `track name "timeline" does not match any other track`,
		},
		"ambiguous track name": {
			tracks: []Track{
				video,
				{Namespace: "other/ns", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
				timeline(DependsOnTrack("video")),
			},
			errMatch: `track name "video" matches 2 tracks; it must match exactly one`,
		},
		"unknown render group": {
			tracks:   []Track{video, timeline(DependsOnRenderGroup(2))},
			errMatch: "tracks[1]: depends[0]: renderGroup 2 does not match any track",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := Catalog{Version: CurrentVersion, Tracks: tt.tracks, PublishTracks: tt.publishTracks}.Validate()
			if tt.errMatch == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestCatalogValidate_AccessibleWindowTimeMatchesAcrossAltGroup(t *testing.T) {
	track := func(name string, altGroup int64, window *int64) Track {
		return Track{Name: name, Packaging: PackagingLOC, IsLive: new(true), AltGroup: new(altGroup), AccessibleWindowTime: window}
	}

	tests := map[string]struct {
		tracks  []Track
		wantErr bool
	}{
		"same window":              {tracks: []Track{track("a", 1, new(int64(30000))), track("b", 1, new(int64(30000)))}},
		"different alt groups":     {tracks: []Track{track("a", 1, new(int64(30000))), track("b", 2, new(int64(60000)))}},
		"both absent":              {tracks: []Track{track("a", 1, nil), track("b", 1, nil)}},
		"different windows":        {tracks: []Track{track("a", 1, new(int64(30000))), track("b", 1, new(int64(60000)))}, wantErr: true},
		"one absent and one set":   {tracks: []Track{track("a", 1, nil), track("b", 1, new(int64(60000)))}, wantErr: true},
		"third track of the group": {tracks: []Track{track("a", 1, new(int64(1))), track("b", 1, new(int64(1))), track("c", 1, new(int64(2)))}, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := Catalog{Version: CurrentVersion, Tracks: tt.tracks}.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "accessibleWindowTime must match the other tracks of altGroup 1")
		})
	}
}

func TestCatalogValidate_InitDataListTypes(t *testing.T) {
	tests := map[string]struct {
		ref      InitDataRef
		errMatch string
	}{
		"inline":                    {ref: InitDataRef{ID: "i", Type: InitDataTypeInline, Data: "AAAA"}},
		"track property":            {ref: InitDataRef{ID: "i", Type: InitDataTypeTrackProperty, Data: "0x0D"}},
		"object property":           {ref: InitDataRef{ID: "i", Type: InitDataTypeObjectProperty, Data: "0F"}},
		"property type not hex":     {ref: InitDataRef{ID: "i", Type: InitDataTypeTrackProperty, Data: "video"}, errMatch: `initDataList[0]: data "video" must be a property type in hex notation`},
		"unknown type":              {ref: InitDataRef{ID: "i", Type: "url", Data: "x"}, errMatch: `initDataList[0]: type "url" must be`},
		"empty type stays accepted": {ref: InitDataRef{ID: "i", Data: "AAAA"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := Catalog{
				Version:      CurrentVersion,
				Tracks:       []Track{{Name: "video", Packaging: PackagingLOC, IsLive: new(true), InitRef: "i"}},
				InitDataList: []InitDataRef{tt.ref},
			}
			err := catalog.Validate()
			if tt.errMatch == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestCatalogValidate_FullTrackNameUniqueness(t *testing.T) {
	tests := map[string]struct {
		tracks  []Track
		wantErr bool
	}{
		"same name in different namespaces": {
			tracks: []Track{
				{Namespace: "a/b", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
				{Namespace: "a/c", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			},
		},
		"declared namespace replaces the inherited one": {
			tracks: []Track{
				{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
				{Namespace: "live", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			},
		},
		"declared namespace equal to the inherited one": {
			tracks: []Track{
				{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
				{Namespace: "catalog-ns", Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := Catalog{Version: CurrentVersion, DefaultNamespace: "catalog-ns", Tracks: tt.tracks}.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicate track identity")
		})
	}
}

// TestCatalogMarshalJSON_EmptyTracksIsAnArray uses the terminating catalog of
// draft-ietf-moq-msf-02 §5.6.13.
func TestCatalogMarshalJSON_EmptyTracksIsAnArray(t *testing.T) {
	generatedAt := int64(1746104606044)
	catalog := Catalog{Version: CurrentVersion, GeneratedAt: &generatedAt, IsComplete: true}

	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":"draft-02","generatedAt":1746104606044,"isComplete":true,"tracks":[]}`, string(data))
}
