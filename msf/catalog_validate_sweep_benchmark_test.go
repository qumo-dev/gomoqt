package msf

import (
	"fmt"
	"testing"
)

// BenchmarkCatalog_Validate_Sweep measures the happy-path (no duplicates,
// no other validation errors) cost of Catalog.Validate as track count N
// sweeps across the N<=16 fast-path threshold PR #332 introduces for the
// duplicate-track-identity check. All tracks are unique, so every entry
// exercises the full comparison loop with no early exit.
func BenchmarkCatalog_Validate_Sweep(b *testing.B) {
	for _, size := range []int{2, 5, 10, 16, 17, 32, 100, 1000} {
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
			catalog := Catalog{Version: 1, DefaultNamespace: "ns1", Tracks: tracks}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := catalog.Validate(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
