package moqt

import (
	"errors"
	"fmt"
)

// SubscribeConfig holds subscription parameters for a track: the
// subscriber's delivery priority, its Max Age, and the range to deliver.
//
// A range is bounded by Positions (moq-lite-06 §3.6): a group, refined by a
// frame index within it. StartGroup/StartFrame name the first frame to
// deliver; EndGroup/EndFrame the end. Two subscriptions abut exactly when the
// first ends where the second starts with the same numbers: a subscriber that
// has frames 0..N-1 of group G caps one subscription at EndGroup = G,
// EndFrame = N and asks another for StartGroup = G, StartFrame = N.
//
// Group order within a track is always newest first; a subscriber that wants
// groups in sequence order reads them in that order (and raises MaxAge so the
// older ones are still delivered when it gets to them).
type SubscribeConfig struct {
	Priority TrackPriority

	// MaxAge is the Subscriber Max Age, in milliseconds: how far behind the
	// newest frame of the latest group a non-latest group may fall before it
	// is dropped from live delivery. It also picks where the subscription
	// starts: the oldest group at or above StartGroup still within MaxAge, so
	// zero starts at the latest group.
	MaxAge uint64

	// StartGroup is an absolute floor: no group below it is delivered. Zero
	// means no floor. A floor above the latest group waits for it.
	StartGroup GroupSequence

	// EndGroup is the last group to deliver, inclusive. Zero means unbounded.
	EndGroup GroupSequence

	// StartFrame is the index of the first frame to deliver within
	// StartGroup; zero delivers it whole. It applies only when delivery
	// begins at StartGroup itself: a start resolved at a later group begins
	// at that group's frame 0. Send zero unless StartGroup is a group you
	// have already begun receiving, since frames cannot be numbered in a
	// group you have not seen.
	StartFrame uint64

	// EndFrame is the exclusive end within EndGroup: frames below it are
	// delivered. Zero delivers EndGroup whole. It must be zero when EndGroup
	// is zero, since an unbounded subscription has no end group.
	EndFrame uint64
}

func (sc SubscribeConfig) String() string {
	return fmt.Sprintf("{ subscriber_priority: %d, max_age_ms: %d, start_group: %d, start_frame: %d, end_group: %d, end_frame: %d }",
		sc.Priority, sc.MaxAge, sc.StartGroup, sc.StartFrame, sc.EndGroup, sc.EndFrame)
}

// errFrameEndWithoutGroupEnd is returned for a range with an EndFrame but no
// EndGroup, which moq-lite-06 §3.6 makes a protocol violation.
var errFrameEndWithoutGroupEnd = errors.New("moqt: EndFrame requires EndGroup")

// validate checks the rules of moq-lite-06 §3.6 that a range can break on its
// own.
func (sc SubscribeConfig) validate() error {
	if sc.EndFrame != 0 && sc.EndGroup == MinGroupSequence {
		return errFrameEndWithoutGroupEnd
	}
	return nil
}
