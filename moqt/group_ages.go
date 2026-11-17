package moqt

import (
	"math/bits"
	"slices"
	"sync"
)

// groupAges expires the groups of one subscription that its Subscriber Max
// Age leaves behind (moq-lite-06 §6.2).
//
// A group's age is measured in frame timestamps alone: from the group's
// reach, the first frame timestamp of the next group by sequence, to the
// newest frame timestamp of the latest group that has a frame. Reach is the
// furthest a group could still present, since frame durations are not on the
// wire, so a group whose successor has presented no frame never expires. Reach
// is exclusive, so an age equal to Max Age already expires the group; a Max
// Age of zero therefore keeps only the latest group.
//
// "Newest" is the most recently written frame of the latest group, not the
// largest timestamp in it.
type groupAges struct {
	// config returns the subscriber's current config, whose MaxAge is the
	// budget in milliseconds.
	config func() *SubscribeConfig
	// timescale converts the budget to timestamp units.
	timescale uint64

	mu sync.Mutex
	// groups holds, in ascending sequence, the groups still open and the
	// closed ones still needed as the reach of an open one.
	groups []groupAge
	// latest is the highest sequence that has presented a frame, and newest
	// the timestamp of its most recent frame.
	latest    GroupSequence
	newest    uint64
	hasLatest bool
}

// groupAge is what groupAges knows of one group.
type groupAge struct {
	seq GroupSequence
	// first is the timestamp of the group's first frame, once hasFirst.
	first    uint64
	hasFirst bool
	// writer is the group's open stream, nil once it has ended.
	writer *GroupWriter
}

// add registers a newly opened group.
func (a *groupAges) add(w *GroupWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i, found := a.find(w.sequence)
	if found {
		a.groups[i].writer = w
		return
	}
	a.groups = slices.Insert(a.groups, i, groupAge{seq: w.sequence, writer: w})
}

// closed records that w's stream has ended. Its first timestamp stays, as
// the reach of the group before it.
func (a *groupAges) closed(w *GroupWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i, found := a.find(w.sequence); found && a.groups[i].writer == w {
		a.groups[i].writer = nil
	}
	a.prune()
}

// observe records a frame w is writing at timestamp ts, and resets every
// other group the frame leaves expired. It allocates nothing.
func (a *groupAges) observe(w *GroupWriter, ts uint64) {
	var expired [4]*GroupWriter
	n := 0

	a.mu.Lock()
	i, found := a.find(w.sequence)
	if !found {
		a.mu.Unlock()
		return
	}
	g := &a.groups[i]
	if !g.hasFirst {
		g.first, g.hasFirst = ts, true
	}
	if !a.hasLatest || w.sequence >= a.latest {
		a.latest, a.newest, a.hasLatest = w.sequence, ts, true
	}

	maxAge := a.config().MaxAge
	reach, hasReach := uint64(0), false
	// Walk down from the newest group, carrying the first timestamp of the
	// group above as the reach of the one below.
	for j := len(a.groups) - 1; j >= 0; j-- {
		g := &a.groups[j]
		if g.writer != nil && g.seq < a.latest && hasReach && n < len(expired) &&
			a.expired(reach, maxAge) {
			expired[n] = g.writer
			n++
			g.writer = nil
		}
		if g.hasFirst {
			reach, hasReach = g.first, true
		}
	}
	if n > 0 {
		a.prune()
	}
	a.mu.Unlock()

	for _, ew := range expired[:n] {
		ew.CancelWrite(ExpiredGroupErrorCode)
	}
}

// expired reports whether a group whose reach is reach is older than maxAge
// milliseconds, relative to the newest frame. Caller MUST hold a.mu.
func (a *groupAges) expired(reach, maxAge uint64) bool {
	if a.newest < reach {
		// Timestamps went backwards; nothing proves the group is behind.
		return false
	}
	age := a.newest - reach
	// age / timescale >= maxAge / 1000, compared in 128 bits so neither
	// product overflows.
	ahi, alo := bits.Mul64(age, 1000)
	bhi, blo := bits.Mul64(maxAge, a.timescale)
	return ahi > bhi || (ahi == bhi && alo >= blo)
}

// find returns the index of seq in a.groups, or where it would be inserted.
// Caller MUST hold a.mu.
func (a *groupAges) find(seq GroupSequence) (int, bool) {
	return slices.BinarySearchFunc(a.groups, seq, func(g groupAge, seq GroupSequence) int {
		switch {
		case g.seq < seq:
			return -1
		case g.seq > seq:
			return 1
		default:
			return 0
		}
	})
}

// prune drops the closed groups below the oldest open one: nothing left
// needs them as a reach. Caller MUST hold a.mu.
func (a *groupAges) prune() {
	i := 0
	for i < len(a.groups)-1 && a.groups[i].writer == nil {
		i++
	}
	if i > 0 {
		a.groups = slices.Delete(a.groups, 0, i)
	}
}
