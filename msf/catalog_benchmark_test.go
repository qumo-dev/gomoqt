package msf

import "testing"

// BenchmarkCatalog_Validate measures the happy-path (no errors) cost of
// Catalog.Validate, where per-track fmt.Sprintf prefix formatting is paid on
// every iteration regardless of whether problems exist.
func BenchmarkCatalog_Validate(b *testing.B) {
	catalog := Catalog{
		Version:          1,
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
