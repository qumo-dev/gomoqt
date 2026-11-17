package moqt

import (
	"errors"
	"fmt"
)

// SubscribeConfig holds subscription parameters for a track: the
// subscriber's delivery priority, its Max Age, and the range of groups to
// deliver.
//
// Group order within a track is always newest first; a subscriber that wants
// groups in sequence order reads them in that order (and raises MaxAge so the
// older ones are still delivered when it gets to them).
//
// moq-lite-06 can also bound a subscription by frame within its start and end
// groups (a Position, §3.6). gomoqt handles that internally: it always
// subscribes to whole groups, and serves a peer that asks for part of a group
// correctly, but exposes no frame-level knob.
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

	// startFrame and endFrame refine StartGroup and EndGroup by frame
	// (moq-lite-06 §3.6), in their wire form: startFrame is the index of the
	// first frame to deliver within StartGroup, and endFrame the exclusive
	// end within EndGroup (0 for the whole group, and always 0 when EndGroup
	// is 0). They are set only from a peer's SUBSCRIBE or SUBSCRIBE_UPDATE;
	// a subscription this endpoint makes sends both as 0.
	startFrame uint64
	endFrame   uint64
}

func (sc SubscribeConfig) String() string {
	return fmt.Sprintf("{ subscriber_priority: %d, max_age_ms: %d, start_group: %d, end_group: %d }",
		sc.Priority, sc.MaxAge, sc.StartGroup, sc.EndGroup)
}

// errFrameEndWithoutGroupEnd is returned for a range with a Frame End but no
// Group End, which moq-lite-06 §3.6 makes a protocol violation.
var errFrameEndWithoutGroupEnd = errors.New("moqt: Frame End requires Group End")

// validate checks a peer's range against the rule of moq-lite-06 §3.6 that a
// range can break on its own: a Frame End without a Group End.
func (sc SubscribeConfig) validate() error {
	if sc.endFrame != 0 && sc.EndGroup == MinGroupSequence {
		return errFrameEndWithoutGroupEnd
	}
	return nil
}
