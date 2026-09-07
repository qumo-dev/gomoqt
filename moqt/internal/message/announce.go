package message

import (
	"io"
)

const (
	ENDED  AnnounceStatus = 0x0
	ACTIVE AnnounceStatus = 0x1
	// LIVE   AnnounceStatus = 0x2
)

type AnnounceStatus byte

// AnnounceMessage is sent on an ANNOUNCE stream.
// Only the broadcast path suffix is carried on the wire; the receiver
// reconstructs the full broadcast path by prepending the requested prefix.
type AnnounceMessage struct {
	AnnounceStatus      AnnounceStatus
	BroadcastPathSuffix string
	HopIDs              []uint64
	// RouteCost is the accumulated route cost of the announcement, in
	// the semantics of draft-lcurley-moq-cluster 5.2 ROUTE_COST: "the
	// marginal cost of subscribing via this advertisement". Units are
	// defined by the deployment's link-cost policy (the cluster draft
	// prices each link at 1 by default; a deployment MAY price links by
	// measured RTT instead). Absent means 0.
	//
	// It is encoded only when non-zero: peers running older versions
	// that reject trailing bytes decode the shorter message unchanged,
	// and senders without a cost contribution keep emitting the exact
	// previous wire format. (Placement as a trailing varint on the
	// moq-lite ANNOUNCE is a gomoqt-local encoding; the cluster draft
	// carries the same value as a KVP parameter on PUBLISH_NAMESPACE /
	// extended NAMESPACE. See gomoqt#409.)
	RouteCost uint64
}

func (am AnnounceMessage) Len() int {
	var l int

	l += VarintLen(uint64(am.AnnounceStatus))
	l += StringLen(am.BroadcastPathSuffix)
	l += VarintLen(uint64(len(am.HopIDs)))
	for _, id := range am.HopIDs {
		l += VarintLen(id)
	}
	if am.RouteCost != 0 {
		l += VarintLen(am.RouteCost)
	}

	return l
}

func (am AnnounceMessage) Encode(w io.Writer) error {
	msgLen := am.Len()

	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, uint64(am.AnnounceStatus))
	b, _ = WriteString(b, am.BroadcastPathSuffix)
	b, _ = WriteVarint(b, uint64(len(am.HopIDs)))
	for _, id := range am.HopIDs {
		b, _ = WriteVarint(b, id)
	}
	if am.RouteCost != 0 {
		b, _ = WriteVarint(b, am.RouteCost)
	}

	_, err := w.Write(b)

	return err
}

func (am *AnnounceMessage) Decode(src io.Reader) error {
	size, err := ReadMessageLength(src)
	if err != nil {
		return err
	}

	if size > MaxMessageSize {
		return ErrMessageTooLarge
	}

	b := make([]byte, size)

	_, err = io.ReadFull(src, b)
	if err != nil {
		return err
	}

	num, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	am.AnnounceStatus = AnnounceStatus(num)
	b = b[n:]

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	am.BroadcastPathSuffix = str
	b = b[n:]

	hopCount, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	b = b[n:]

	allocCap := min(hopCount, uint64(len(b)))
	am.HopIDs = make([]uint64, 0, allocCap)
	for range hopCount {
		num, n, err = ReadVarint(b)
		if err != nil {
			return err
		}
		am.HopIDs = append(am.HopIDs, num)
		b = b[n:]
	}

	// PathCost is a trailing field that older senders omit; read it only
	// when bytes remain, and keep rejecting any other trailing garbage.
	if len(b) != 0 {
		cost, n, err := ReadVarint(b)
		if err != nil {
			return err
		}
		am.RouteCost = cost
		b = b[n:]
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	return nil
}
