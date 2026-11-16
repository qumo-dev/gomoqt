package msf

import (
	"github.com/qumo-dev/gomoqt/moqt"
)

var _ moqt.TrackHandler = (*fakeTrackHandler)(nil)

// fakeTrackHandler is a fake implementation of moqt.TrackHandler that records calls.
type fakeTrackHandler struct {
	serveTrackFunc func(tw *moqt.TrackWriter)
}

func (m *fakeTrackHandler) ServeTrack(tw *moqt.TrackWriter) {
	if m.serveTrackFunc != nil {
		m.serveTrackFunc(tw)
	}
}
