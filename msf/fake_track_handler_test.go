package msf

import (
	"github.com/okdaichi/gomoqt/moqt"
)

var _ moqt.TrackHandler = (*FakeTrackHandler)(nil)

// FakeTrackHandler is a fake implementation of moqt.TrackHandler that records calls.
type FakeTrackHandler struct {
	ServeTrackFunc func(tw *moqt.TrackWriter)
}

func (m *FakeTrackHandler) ServeTrack(tw *moqt.TrackWriter) {
	if m.ServeTrackFunc != nil {
		m.ServeTrackFunc(tw)
	}
}
