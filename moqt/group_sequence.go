package moqt

import "fmt"

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
	return fmt.Sprintf("%d", gs)
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

// groupSequenceToWire converts a group sequence into the wire form used by draft03.
// Zero stays zero so it can continue to represent an omitted field.
func groupSequenceToWire(gs GroupSequence) uint64 {
	if gs == MinGroupSequence {
		return 0
	}

	return uint64(gs) + 1
}

// groupSequenceFromWire converts a draft03 wire value into a group sequence.
// Zero stays zero so it can continue to represent an omitted field.
func groupSequenceFromWire(v uint64) GroupSequence {
	if v == 0 {
		return MinGroupSequence
	}

	return GroupSequence(v - 1)
}

// boolToWireFlag converts a boolean into the draft03 uint8 flag form.
// false => 0, true => 1.
func boolToWireFlag(v bool) uint8 {
	if v {
		return 1
	}

	return 0
}

// boolFromWireFlag converts a draft03 uint8 flag into a boolean.
// Any non-zero value is treated as true.
func boolFromWireFlag(v uint8) bool {
	return v != 0
}
