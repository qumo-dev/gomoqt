package msf

import (
	"fmt"
	"testing"
)

// BenchmarkCatalog_ApplyDelta measures Catalog.ApplyDelta for a delta that
// removes one track, adds one and clones one, against base catalogs that
// straddle the N<=16 fast path in Catalog.Validate.
func BenchmarkCatalog_ApplyDelta(b *testing.B) {
	generatedAt := int64(1746104606044)
	for _, size := range []int{5, 16, 32, 100} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			tracks := make([]Track, size)
			for i := range size {
				tracks[i] = Track{
					Name:      fmt.Sprintf("track-%d", i),
					Packaging: PackagingLOC,
					IsLive:    new(true),
				}
			}
			base := Catalog{Version: 1, Tracks: tracks}
			delta := CatalogDelta{
				GeneratedAt:  &generatedAt,
				RemoveTracks: []TrackRef{{Name: "track-0"}},
				AddTracks:    []Track{{Name: "added", Packaging: PackagingLOC, IsLive: new(true)}},
				CloneTracks:  []TrackClone{{ParentName: "track-1", Track: Track{Name: "cloned"}}},
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := base.ApplyDelta(delta); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
