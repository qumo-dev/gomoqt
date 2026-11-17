package msf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BenchmarkCatalog_Validate measures the happy-path (no errors) cost of
// Catalog.Validate, where per-track fmt.Sprintf prefix formatting is paid on
// every iteration regardless of whether problems exist.
func BenchmarkCatalog_Validate(b *testing.B) {
	catalog := Catalog{
		Version:          CurrentVersion,
		DefaultNamespace: "live/demo",
		Tracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)},
			{Name: "text", Packaging: PackagingLOC, IsLive: new(false)},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = catalog.Validate()
	}
}

// BenchmarkCatalogDelta_Validate measures the happy-path cost of
// CatalogDelta.Validate (deferred per-entry prefix formatting).
func BenchmarkCatalogDelta_Validate(b *testing.B) {
	delta := CatalogDelta{
		// draft-02 requires generatedAt; without it this would time the error path.
		GeneratedAt: new(int64(1746104606044)),
		AddTracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true)},
			{Name: "audio", Packaging: PackagingLOC, IsLive: new(false)},
		},
		RemoveTracks: []TrackRef{{Name: "old"}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = delta.Validate()
	}
}

func TestCatalog_Validate_SmallCatalogDoesNotAllocate(t *testing.T) {
	catalog := Catalog{
		Version:          CurrentVersion,
		DefaultNamespace: "live/demo",
		Tracks: []Track{
			{Name: "video", Packaging: PackagingLOC, IsLive: new(true), InitRef: "v"},
			{Name: "audio", Packaging: PackagingLOC, IsLive: new(true)},
		},
		InitDataList: []InitDataRef{{ID: "v", Type: "inline", Data: "AAAA"}},
	}
	require.NoError(t, catalog.Validate())

	allocs := testing.AllocsPerRun(100, func() { _ = catalog.Validate() })

	assert.Zero(t, allocs, "a valid catalog of 16 tracks or fewer validates without allocating")
}
