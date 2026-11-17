package msf

import (
	"encoding/json"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testVideoTrack returns a loc video track carrying the fields §5.2.4.1
// requires for that (packaging, role) pair.
func testVideoTrack(tb testing.TB, namespace, name string) Track {
	tb.Helper()
	return Track{
		Namespace: namespace,
		Name:      name,
		Packaging: PackagingLOC,
		Role:      RoleVideo,
		IsLive:    new(true),
		Codec:     "av01.0.08M.10.0.110.09",
		Bitrate:   new(int64(5000000)),
		Framerate: new(int64(30)),
		Width:     new(int64(1920)),
		Height:    new(int64(1080)),
	}
}

// TestCatalogApplyDelta_SpecExample applies the delta of draft-ietf-moq-msf-02
// §5.6.4: an update, an add and a clone from a parent in another namespace.
func TestCatalogApplyDelta_SpecExample(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{
			testVideoTrack(t, "", "video-1080"),
			testVideoTrack(t, "example.com/custom", "video-1080"),
		},
	}
	delta, err := ParseCatalogDeltaString(`{
		"generatedAt": 1746104606044,
		"deltaUpdate": [
			{"op": "update", "tracks": [{"parentName": "video-1080", "bitrate": 4000000}]},
			{"op": "add", "tracks": [{
				"name": "slides", "packaging": "loc", "isLive": true, "role": "video",
				"codec": "av01.0.08M.10.0.110.09", "width": 1920, "height": 1080,
				"framerate": 15, "bitrate": 750000, "renderGroup": 1
			}]},
			{"op": "clone", "tracks": [{
				"parentName": "video-1080", "parentNamespace": "example.com/custom",
				"name": "video-720", "width": 1280, "height": 720, "bitrate": 600000
			}]}
		]
	}`)
	require.NoError(t, err)
	require.NoError(t, delta.Validate())

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)

	require.Len(t, updated.Tracks, 4)
	require.NotNil(t, updated.GeneratedAt)
	assert.Equal(t, int64(1746104606044), *updated.GeneratedAt)
	assert.Equal(t, int64(4000000), *updated.Tracks[0].Bitrate, "update overrides the inherited-namespace track")
	assert.Equal(t, int64(30), *updated.Tracks[0].Framerate, "update keeps undeclared fields")
	assert.Equal(t, int64(5000000), *updated.Tracks[1].Bitrate, "update leaves the same name in another namespace alone")
	assert.Equal(t, "slides", updated.Tracks[2].Name)
	assert.Equal(t, "video-720", updated.Tracks[3].Name)
	assert.Equal(t, "example.com/custom", updated.Tracks[3].Namespace)
	assert.Equal(t, int64(1280), *updated.Tracks[3].Width)
	assert.Equal(t, int64(30), *updated.Tracks[3].Framerate)
}

func TestCatalogApplyDelta_UpdateErrors(t *testing.T) {
	ended := testVideoTrack(t, "", "ended")
	ended.IsLive = new(false)
	base := Catalog{
		Version: CurrentVersion,
		Tracks:  []Track{testVideoTrack(t, "", "video"), testVideoTrack(t, "", "audio"), ended},
	}

	tests := map[string]struct {
		update   TrackUpdate
		errMatch string
	}{
		"unknown track": {
			update:   TrackUpdate{ParentName: "missing", Track: Track{Bitrate: new(int64(1))}},
			errMatch: "cannot update unknown track",
		},
		"unknown namespace": {
			update:   TrackUpdate{ParentName: "video", ParentNamespace: "other/ns", Track: Track{Bitrate: new(int64(1))}},
			errMatch: "cannot update unknown track",
		},
		"isLive false to true": {
			update:   TrackUpdate{ParentName: "ended", Track: Track{IsLive: new(true)}},
			errMatch: "isLive must not change from false to true",
		},
		"rename onto another track": {
			update:   TrackUpdate{ParentName: "video", Track: Track{Name: "audio"}},
			errMatch: "into duplicate track",
		},
		"result fails validation": {
			update:   TrackUpdate{ParentName: "video", Track: Track{TrackDuration: new(int64(1000))}},
			errMatch: "trackDuration must not be present when isLive is true",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			delta := CatalogDelta{GeneratedAt: new(int64(1)), UpdateTracks: []TrackUpdate{tt.update}}
			_, err := base.ApplyDelta(delta)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestCatalogApplyDelta_UpdateRenamesAndEndsTrack(t *testing.T) {
	base := Catalog{Version: CurrentVersion, Tracks: []Track{testVideoTrack(t, "", "video")}}
	delta := CatalogDelta{
		GeneratedAt:  new(int64(1)),
		UpdateTracks: []TrackUpdate{{ParentName: "video", Track: Track{Name: "video-hd", IsLive: new(false)}}},
	}

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	require.Len(t, updated.Tracks, 1)
	assert.Equal(t, "video-hd", updated.Tracks[0].Name)
	assert.False(t, *updated.Tracks[0].IsLive)
	// The base catalog is not mutated.
	assert.Equal(t, "video", base.Tracks[0].Name)
	assert.True(t, *base.Tracks[0].IsLive)
}

// TestCatalogApplyDelta_CodeBuiltUpdateFollowsAdd checks that a delta built in
// code applies its update operations after its adds, so an update can target
// a track the same delta adds.
func TestCatalogApplyDelta_CodeBuiltUpdateFollowsAdd(t *testing.T) {
	base := Catalog{Version: CurrentVersion, Tracks: []Track{testVideoTrack(t, "", "video")}}
	delta := CatalogDelta{
		GeneratedAt:  new(int64(1)),
		UpdateTracks: []TrackUpdate{{ParentName: "slides", Track: Track{Bitrate: new(int64(1))}}},
		AddTracks:    []Track{testVideoTrack(t, "", "slides")},
	}

	updated, err := base.ApplyDelta(delta)
	require.NoError(t, err)
	require.Len(t, updated.Tracks, 2)
	assert.Equal(t, int64(1), *updated.Tracks[1].Bitrate)

	data, err := json.Marshal(delta)
	require.NoError(t, err)
	assert.JSONEq(t, `{"generatedAt":1,"deltaUpdate":[
		{"op":"add","tracks":[{"name":"slides","packaging":"loc","role":"video","isLive":true,
			"codec":"av01.0.08M.10.0.110.09","bitrate":5000000,"framerate":30,"width":1920,"height":1080}]},
		{"op":"update","tracks":[{"parentName":"slides","bitrate":1}]}]}`, string(data))
}

func TestCatalogApplyDelta_RemovingADependencyInvalidates(t *testing.T) {
	base := Catalog{
		Version: CurrentVersion,
		Tracks: []Track{
			testVideoTrack(t, "", "video"),
			{Name: "timeline", Packaging: PackagingMediaTimeline, IsLive: new(true), Depends: []Dependency{DependsOnTrack("video")}},
		},
	}
	delta := CatalogDelta{GeneratedAt: new(int64(1)), RemoveTracks: []TrackRef{{Name: "video"}}}

	_, err := base.ApplyDelta(delta)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `track name "video" does not match any other track`)
}

func TestCatalogDeltaValidate_RequiresGeneratedAt(t *testing.T) {
	delta := CatalogDelta{RemoveTracks: []TrackRef{{Name: "video"}}}

	err := delta.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delta catalog must include generatedAt")
}

func TestCatalogDeltaValidate_EntryErrors(t *testing.T) {
	tests := map[string]struct {
		delta    CatalogDelta
		errMatch string
	}{
		"update without parentName": {
			delta:    CatalogDelta{UpdateTracks: []TrackUpdate{{Track: Track{Bitrate: new(int64(1))}}}},
			errMatch: "update[0]: parentName is required for update tracks",
		},
		"update with malformed parentNamespace": {
			delta:    CatalogDelta{UpdateTracks: []TrackUpdate{{ParentName: "video", ParentNamespace: "a//b"}}},
			errMatch: "update[0]: parentNamespace: namespace \"a//b\" contains an empty field",
		},
		"clone with malformed parentNamespace": {
			delta:    CatalogDelta{CloneTracks: []TrackClone{{Track: Track{Name: "v2"}, ParentName: "video", ParentNamespace: "/a"}}},
			errMatch: "clone[0]: parentNamespace",
		},
		"remove with malformed namespace": {
			delta:    CatalogDelta{RemoveTracks: []TrackRef{{Name: "video", Namespace: "a/"}}},
			errMatch: "remove[0]: namespace \"a/\" contains an empty field",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.delta.GeneratedAt = new(int64(1))
			err := tt.delta.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)
		})
	}
}

func TestParseCatalogDelta_RejectsRootFields(t *testing.T) {
	for _, field := range []string{
		`"version":"draft-02"`,
		`"isComplete":true`,
		`"tracks":[]`,
		`"publishTracks":[]`,
		`"initDataList":[]`,
	} {
		t.Run(field, func(t *testing.T) {
			_, err := ParseCatalogDeltaString(`{"generatedAt":1,` + field + `,"deltaUpdate":[]}`)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is not allowed in a delta catalog")
		})
	}
}

func TestParseCatalogDelta_KeepsCustomRootFields(t *testing.T) {
	delta, err := ParseCatalogDeltaString(`{"generatedAt":1,"com.example":7,"deltaUpdate":[{"op":"remove","tracks":[{"name":"v"}]}]}`)
	require.NoError(t, err)
	assert.JSONEq(t, `7`, string(delta.ExtraFields["com.example"]))
}

func TestTrackUpdate_JSONRoundTrip(t *testing.T) {
	input := `{"parentName":"video-1080","parentNamespace":"example.com/custom","bitrate":4000000,"com.example":true}`

	var update TrackUpdate
	require.NoError(t, json.Unmarshal([]byte(input), &update))
	assert.Equal(t, "video-1080", update.ParentName)
	assert.Equal(t, "example.com/custom", update.ParentNamespace)
	require.NotNil(t, update.Bitrate)
	assert.Equal(t, int64(4000000), *update.Bitrate)
	assert.Empty(t, update.Name)
	assert.NotContains(t, update.ExtraFields, "parentName")

	data, err := json.Marshal(update)
	require.NoError(t, err)
	assert.JSONEq(t, input, string(data))
}

func TestTrackUpdate_UnmarshalJSON_Errors(t *testing.T) {
	tests := map[string]string{
		"not an object":         `[]`,
		"bad parentName":        `{"parentName": 1}`,
		"bad parentNamespace":   `{"parentName": "v", "parentNamespace": 1}`,
		"bad overridden field":  `{"parentName": "v", "bitrate": "fast"}`,
		"bad depends entry":     `{"parentName": "v", "depends": [true]}`,
		"bad accessible window": `{"parentName": "v", "accessibleWindowTime": "1"}`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			var update TrackUpdate
			assert.Error(t, json.Unmarshal([]byte(input), &update))
		})
	}
}

func TestCatalogApplyDelta_DefaultNamespaceChangeChecksPublishTracks(t *testing.T) {
	tests := map[string]struct {
		base     Catalog
		errMatch string
	}{
		"publish track inherits": {
			base: Catalog{
				Version:          CurrentVersion,
				DefaultNamespace: "live/demo",
				PublishTracks:    []Track{{Name: "events", Packaging: PackagingEventTimeline, EventType: "com.example.e", IsLive: new(true)}},
			},
			errMatch: "cannot change default namespace",
		},
		"every track declares a namespace": {
			base: Catalog{
				Version:          CurrentVersion,
				DefaultNamespace: "live/demo",
				Tracks:           []Track{{Namespace: "live/demo", Name: "video", Packaging: PackagingLOC, IsLive: new(true)}},
				PublishTracks:    []Track{{Namespace: "live/demo", Name: "events", Packaging: PackagingEventTimeline, EventType: "com.example.e", IsLive: new(true)}},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			delta := CatalogDelta{
				GeneratedAt:      new(int64(1)),
				DefaultNamespace: "live/updated",
				AddTracks:        []Track{{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)}},
			}
			updated, err := tt.base.ApplyDelta(delta)
			if tt.errMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "live/updated", updated.DefaultNamespace)
		})
	}
}

func TestCatalogApplyDelta_RejectsCompleteCatalog(t *testing.T) {
	base := Catalog{Version: CurrentVersion, IsComplete: true, Tracks: []Track{testVideoTrack(t, "", "video")}}
	deltas := map[string]CatalogDelta{
		"add":    {GeneratedAt: new(int64(1)), AddTracks: []Track{testVideoTrack(t, "", "slides")}},
		"remove": {GeneratedAt: new(int64(1)), RemoveTracks: []TrackRef{{Name: "video"}}},
		"update": {GeneratedAt: new(int64(1)), UpdateTracks: []TrackUpdate{{ParentName: "video", Track: Track{IsLive: new(false)}}}},
	}

	for name, delta := range deltas {
		t.Run(name, func(t *testing.T) {
			_, err := base.ApplyDelta(delta)
			assert.ErrorIs(t, err, ErrCatalogComplete)
		})
	}
}

func TestParseCatalogDelta_RepeatedDeltaUpdateLastWins(t *testing.T) {
	delta, err := ParseCatalogDeltaString(`{"generatedAt":1,
		"deltaUpdate":[{"op":"add","tracks":[{"name":"a","packaging":"loc","isLive":true}]}],
		"deltaUpdate":[{"op":"remove","tracks":[{"name":"b"}]}]}`)
	require.NoError(t, err)

	assert.Empty(t, delta.AddTracks)
	assert.Equal(t, []TrackRef{{Name: "b", ExtraFields: map[string]json.RawMessage{}}}, delta.RemoveTracks)
	data, err := json.Marshal(delta)
	require.NoError(t, err)
	assert.JSONEq(t, `{"generatedAt":1,"deltaUpdate":[{"op":"remove","tracks":[{"name":"b"}]}]}`, string(data))
}

func TestParseCatalog_RepeatedTracksLastWins(t *testing.T) {
	catalog, err := ParseCatalogString(`{"version":"draft-02",
		"tracks":[{"name":"a","packaging":"loc","isLive":true}],
		"tracks":[{"name":"b","packaging":"loc","isLive":true}]}`)
	require.NoError(t, err)
	require.Len(t, catalog.Tracks, 1)
	assert.Equal(t, "b", catalog.Tracks[0].Name)
}

// randomTrack returns a loc or mediatimeline track named from a small pool,
// with random render group, depends and isLive, so that random deltas often
// collide, dangle, or break references.
func randomTrack(rng *rand.Rand, names []string) Track {
	track := Track{Name: names[rng.IntN(len(names))], Packaging: PackagingLOC, IsLive: new(rng.IntN(2) == 0)}
	if rng.IntN(3) == 0 {
		track.Namespace = "other/ns"
	}
	if rng.IntN(2) == 0 {
		track.RenderGroup = new(int64(rng.IntN(2)))
	}
	if rng.IntN(3) == 0 {
		track.Packaging = PackagingMediaTimeline
		track.Depends = []Dependency{DependsOnTrack(names[rng.IntN(len(names))])}
		if rng.IntN(2) == 0 {
			track.Depends = []Dependency{DependsOnRenderGroup(int64(rng.IntN(2)))}
		}
	}
	return track
}

// TestCatalogApplyDelta_RandomDeltasKeepCatalogValid is a property test:
// over many random base catalogs and deltas, ApplyDelta either fails or
// returns a catalog that validates, and never changes its receiver.
func TestCatalogApplyDelta_RandomDeltasKeepCatalogValid(t *testing.T) {
	rng := rand.New(rand.NewPCG(529, 2))
	names := []string{"a", "b", "c", "d"}
	applied, valid := 0, 0

	for range 5000 {
		base := Catalog{Version: CurrentVersion, DefaultNamespace: "live/demo"}
		for range rng.IntN(5) {
			base.Tracks = append(base.Tracks, randomTrack(rng, names))
		}
		if base.Validate() != nil {
			continue
		}
		valid++

		delta := CatalogDelta{GeneratedAt: new(int64(1))}
		for range 1 + rng.IntN(3) {
			name := names[rng.IntN(len(names))]
			switch rng.IntN(4) {
			case 0:
				delta.AddTracks = append(delta.AddTracks, randomTrack(rng, names))
			case 1:
				delta.RemoveTracks = append(delta.RemoveTracks, TrackRef{Name: name})
			case 2:
				clone := TrackClone{ParentName: name, Track: Track{Name: names[rng.IntN(len(names))]}}
				if rng.IntN(2) == 0 {
					clone.Depends = []Dependency{DependsOnTrack(names[rng.IntN(len(names))])}
				}
				delta.CloneTracks = append(delta.CloneTracks, clone)
			default:
				update := TrackUpdate{ParentName: name, Track: Track{IsLive: new(rng.IntN(2) == 0)}}
				if rng.IntN(2) == 0 {
					update.RenderGroup = new(int64(rng.IntN(3)))
				}
				delta.UpdateTracks = append(delta.UpdateTracks, update)
			}
		}

		if _, err := base.Clone().ApplyDelta(delta); err == nil {
			applied++
		}
		assertApplyDeltaProperty(t, base, delta)
	}

	// Guard against a generator that never exercises the success path.
	assert.Greater(t, valid, 1000)
	assert.Greater(t, applied, 100)
}
