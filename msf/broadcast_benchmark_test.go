package msf

import (
	"fmt"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt"
)

func BenchmarkBroadcastRegisterTrack(b *testing.B) {
	b.ReportAllocs()

	handler := moqt.TrackHandlerFunc(func(tw *moqt.TrackWriter) {})

	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			broadcast, err := NewBroadcast(Catalog{Version: 1})
			if err != nil {
				b.Fatal(err)
			}

			// Pre-populate
			for i := 0; i < size; i++ {
				err = broadcast.RegisterTrack(Track{
					Name:      fmt.Sprintf("track-%d", i),
					Namespace: "ns1",
					Packaging: PackagingLOC,
					IsLive:    new(false),
				}, handler)
				if err != nil {
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
				// Registering a new track will do a linear scan over 'size' elements
				// then append.
				err = broadcast.RegisterTrack(newTrack, handler)
				if err != nil {
					b.Fatal(err)
				}

				b.StopTimer()
				// Remove to keep the size the same for the next iteration
				broadcast.RemoveTrack(moqt.TrackName(newTrack.Name))
				b.StartTimer()
			}
		})
	}
}
