package msf

import (
	"slices"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt"
)

var _ moqt.TrackHandler = (*fakeTrackHandler)(nil)

// fakeTrackHandler is a moqt.TrackHandler that records each TrackWriter it is
// asked to serve and does nothing else. A server may call ServeTrack from
// several goroutines, so the record is mutex-guarded.
type fakeTrackHandler struct {
	mu     sync.Mutex
	served []*moqt.TrackWriter
}

func (f *fakeTrackHandler) ServeTrack(tw *moqt.TrackWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.served = append(f.served, tw)
}

// servedTracks returns a copy of the TrackWriters served so far, in order.
func (f *fakeTrackHandler) servedTracks() []*moqt.TrackWriter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.served)
}
