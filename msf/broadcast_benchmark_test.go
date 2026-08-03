package msf

import (
	"fmt"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt"
)

// BenchmarkBroadcastSetCatalog_SameNames measures SetCatalog replacing a
// catalog with an equal-size one holding IDENTICAL track names (only
// staleTrackNamesLocked's "found in map" path exercised; no stale names
// result). This straddles the N<=16 stack-array fast path in
// staleTrackNamesLocked/validateCatalogForBroadcast and the map fallback
// above it, guarding both against regression.
func BenchmarkBroadcastSetCatalog_SameNames(b *testing.B) {
	b.ReportAllocs()

	for _, size := range []int{5, 10, 16, 32, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			tracks := make([]Track, size)
			for i := range size {
				tracks[i] = Track{
					Name:      fmt.Sprintf("track-%d", i),
					Namespace: "ns1",
					Packaging: PackagingLOC,
					IsLive:    new(false),
				}
			}
			catalog := Catalog{Version: 1, Tracks: tracks}

			broadcast, err := NewBroadcast(catalog)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := broadcast.SetCatalog(catalog); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBroadcastSetCatalog_AllStale measures SetCatalog replacing a
// catalog with an equal-size one holding ENTIRELY DIFFERENT track names —
// the worst case for staleTrackNamesLocked (every old name misses the new
// active-names set) and validateCatalogForBroadcast's happy path (every
// comparison in the O(N^2) fast path runs to completion without an early
// duplicate hit).
func BenchmarkBroadcastSetCatalog_AllStale(b *testing.B) {
	b.ReportAllocs()

	for _, size := range []int{5, 10, 16, 32, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			oldTracks := make([]Track, size)
			for i := range size {
				oldTracks[i] = Track{
					Name:      fmt.Sprintf("old-track-%d", i),
					Namespace: "ns1",
					Packaging: PackagingLOC,
					IsLive:    new(false),
				}
			}
			oldCatalog := Catalog{Version: 1, Tracks: oldTracks}

			newTracks := make([]Track, size)
			for i := range size {
				newTracks[i] = Track{
					Name:      fmt.Sprintf("new-track-%d", i),
					Namespace: "ns1",
					Packaging: PackagingLOC,
					IsLive:    new(false),
				}
			}
			newCatalog := Catalog{Version: 1, Tracks: newTracks}

			broadcast, err := NewBroadcast(oldCatalog)
			if err != nil {
				b.Fatal(err)
			}
			// Alternate old/new each call so b.catalog.Tracks is always
			// non-empty and always disjoint from the incoming catalog.
			catalogs := [2]Catalog{oldCatalog, newCatalog}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := broadcast.SetCatalog(catalogs[i%2]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBroadcastRegisterTrack measures repeatedly registering-then-removing
// the same new track against a catalog holding `size` other tracks. Exercises
// validateCatalogForBroadcast (called from RegisterTrack).
func BenchmarkBroadcastRegisterTrack(b *testing.B) {
	b.ReportAllocs()

	handler := moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) {})

	for _, size := range []int{5, 10, 16, 32, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			broadcast, err := NewBroadcast(Catalog{Version: 1})
			if err != nil {
				b.Fatal(err)
			}

			for i := range size {
				if err := broadcast.RegisterTrack(Track{
					Name:      fmt.Sprintf("track-%d", i),
					Namespace: "ns1",
					Packaging: PackagingLOC,
					IsLive:    new(false),
				}, handler); err != nil {
					b.Fatal(err)
				}
			}

			newTrack := Track{
				Name:      fmt.Sprintf("track-%d", size),
				Namespace: "ns1",
				Packaging: PackagingLOC,
				IsLive:    new(false),
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := broadcast.RegisterTrack(newTrack, handler); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				broadcast.RemoveTrack(moqt.TrackName(newTrack.Name))
				b.StartTimer()
			}
		})
	}
}
