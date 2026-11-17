package moqt

import "strconv"

const (
	// MinGroupSequence is the smallest valid sequence number for a group.
	MinGroupSequence GroupSequence = 0
	// MaxGroupSequence is the largest valid sequence number for a group.
	// It is set to 2^62 - 1, the largest sequence number that can be used in a group.
	MaxGroupSequence GroupSequence = 0x3FFFFFFFFFFFFFFF
)

type GroupSequence uint64

// String returns the string representation of the group sequence number.
func (gs GroupSequence) String() string {
	return strconv.FormatUint(uint64(gs), 10)
}

// Next returns the next sequence number in the group sequence.
// If the current sequence is at the maximum, it wraps around to the minimum.
func (gs GroupSequence) Next() GroupSequence {
	if gs == MinGroupSequence {
		return 1
	}

	if gs == MaxGroupSequence {
		// Wrap to the first sequence value
		return MinGroupSequence
	}

	return gs + 1
}

// groupSequenceToWire converts a group sequence into the +1 wire form used by
// SUBSCRIBE Group End (Group Start is a plain floor since moq-lite-06).
// Zero stays zero so it can continue to represent an omitted field.
func groupSequenceToWire(gs GroupSequence) uint64 {
	if gs == MinGroupSequence {
		return 0
	}

	return uint64(gs) + 1
}

// groupSequenceFromWire converts a +1-encoded wire value into a group sequence.
// Zero stays zero so it can continue to represent an omitted field.
func groupSequenceFromWire(v uint64) GroupSequence {
	if v == 0 {
		return MinGroupSequence
	}

	return GroupSequence(v - 1)
}

// subscribeRangeToWire encodes a config's group range as SUBSCRIBE and
// SUBSCRIBE_UPDATE carry it: Group Start plain, Group End +1. Frame Start and
// Frame End are left 0: gomoqt asks for whole groups, even with a config
// taken from a peer's subscription (as a relay forwarding
// TrackWriter.TrackConfig upstream passes one).
func subscribeRangeToWire(c *SubscribeConfig) (groupStart, groupEnd uint64) {
	return uint64(c.StartGroup), groupSequenceToWire(c.EndGroup)
}

// subscribeConfigFromWire builds the config a SUBSCRIBE or SUBSCRIBE_UPDATE
// carries.
func subscribeConfigFromWire(priority uint8, maxAge, groupStart, groupEnd, frameStart, frameEnd uint64) *SubscribeConfig {
	return &SubscribeConfig{
		Priority:   TrackPriority(priority),
		MaxAge:     maxAge,
		StartGroup: GroupSequence(groupStart),
		EndGroup:   groupSequenceFromWire(groupEnd),
		startFrame: frameStart,
		endFrame:   frameEnd,
	}
}
