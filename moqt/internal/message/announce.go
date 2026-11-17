package message

import (
	"io"
)

// Type tags for the announcements a publisher sends on the Announce Stream
// after ANNOUNCE_OK (moq-lite-06 §7.6–7.8). Each message's Encode writes its
// own tag; its Decode expects the caller to have consumed it with
// ReadMessageType.
const (
	MessageTypeAnnounceStart  uint64 = 0x0
	MessageTypeAnnounceEnd    uint64 = 0x1
	MessageTypeAnnounceUpdate uint64 = 0x2
)

// AnnounceStartMessage advertises a route: a claim that paths under a prefix
// can be served. Each one implicitly assigns the next Announce ID on the
// stream. Only the suffix after the requested prefix is carried; an empty
// suffix advertises the requested prefix itself.
//
// Wire format:
//
//	ANNOUNCE_START Message {
//	  Type (i) = 0x0
//	  Message Length (i)
//	  Route Prefix Suffix (s),
//	  Hop Count (i),
//	  Hop ID (i) ...,
//	  Warm Route Cost (i),
//	  Cold Route Cost (i),
//	}
type AnnounceStartMessage struct {
	RoutePrefixSuffix string
	HopIDs            []uint64
	WarmRouteCost     uint64
	ColdRouteCost     uint64
}

func (am AnnounceStartMessage) Len() int {
	return StringLen(am.RoutePrefixSuffix) + hopListLen(am.HopIDs) +
		VarintLen(am.WarmRouteCost) + VarintLen(am.ColdRouteCost)
}

// Encode writes the Type tag and the message in one write.
func (am AnnounceStartMessage) Encode(w io.Writer) error {
	msgLen := am.Len()
	b := make([]byte, 0, 1+msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteVarint(b, MessageTypeAnnounceStart)
	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, am.RoutePrefixSuffix)
	b = appendHopList(b, am.HopIDs)
	b, _ = WriteVarint(b, am.WarmRouteCost)
	b, _ = WriteVarint(b, am.ColdRouteCost)

	_, err := w.Write(b)
	return err
}

// Decode reads the message after its Type tag.
func (am *AnnounceStartMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	str, n, err := ReadString(b)
	if err != nil {
		return err
	}
	am.RoutePrefixSuffix = str
	b = b[n:]

	am.HopIDs, b, err = readHopList(b)
	if err != nil {
		return err
	}

	am.WarmRouteCost, am.ColdRouteCost, b, err = readRouteCosts(b)
	if err != nil {
		return err
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}
	return nil
}

// AnnounceEndMessage retracts a route started on the stream, by its
// Announce ID, which is then retired.
//
// Wire format:
//
//	ANNOUNCE_END Message {
//	  Type (i) = 0x1
//	  Message Length (i)
//	  Announce ID (i)
//	}
type AnnounceEndMessage struct {
	AnnounceID uint64
}

func (am AnnounceEndMessage) Len() int {
	return VarintLen(am.AnnounceID)
}

// Encode writes the Type tag and the message in one write.
func (am AnnounceEndMessage) Encode(w io.Writer) error {
	msgLen := am.Len()
	b := make([]byte, 0, 1+msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteVarint(b, MessageTypeAnnounceEnd)
	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, am.AnnounceID)

	_, err := w.Write(b)
	return err
}

// Decode reads the message after its Type tag.
func (am *AnnounceEndMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	id, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	am.AnnounceID = id
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}
	return nil
}

// AnnounceUpdateMessage atomically replaces the metadata of a route started
// on the stream, by its Announce ID, which stays live. The route's prefix is
// unchanged.
//
// Wire format:
//
//	ANNOUNCE_UPDATE Message {
//	  Type (i) = 0x2
//	  Message Length (i)
//	  Announce ID (i),
//	  Hop Count (i),
//	  Hop ID (i) ...,
//	  Warm Route Cost (i),
//	  Cold Route Cost (i),
//	}
type AnnounceUpdateMessage struct {
	AnnounceID    uint64
	HopIDs        []uint64
	WarmRouteCost uint64
	ColdRouteCost uint64
}

func (am AnnounceUpdateMessage) Len() int {
	return VarintLen(am.AnnounceID) + hopListLen(am.HopIDs) +
		VarintLen(am.WarmRouteCost) + VarintLen(am.ColdRouteCost)
}

// Encode writes the Type tag and the message in one write.
func (am AnnounceUpdateMessage) Encode(w io.Writer) error {
	msgLen := am.Len()
	b := make([]byte, 0, 1+msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteVarint(b, MessageTypeAnnounceUpdate)
	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteVarint(b, am.AnnounceID)
	b = appendHopList(b, am.HopIDs)
	b, _ = WriteVarint(b, am.WarmRouteCost)
	b, _ = WriteVarint(b, am.ColdRouteCost)

	_, err := w.Write(b)
	return err
}

// Decode reads the message after its Type tag.
func (am *AnnounceUpdateMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	id, n, err := ReadVarint(b)
	if err != nil {
		return err
	}
	am.AnnounceID = id
	b = b[n:]

	am.HopIDs, b, err = readHopList(b)
	if err != nil {
		return err
	}

	am.WarmRouteCost, am.ColdRouteCost, b, err = readRouteCosts(b)
	if err != nil {
		return err
	}

	if len(b) != 0 {
		return ErrMessageTooShort
	}
	return nil
}

func hopListLen(ids []uint64) int {
	l := VarintLen(uint64(len(ids)))
	for _, id := range ids {
		l += VarintLen(id)
	}
	return l
}

func appendHopList(b []byte, ids []uint64) []byte {
	b, _ = WriteVarint(b, uint64(len(ids)))
	for _, id := range ids {
		b, _ = WriteVarint(b, id)
	}
	return b
}

// readHopList reads a Hop Count and that many Hop IDs. A count larger than
// the bytes left cannot match the entries that follow, and is rejected before
// anything is allocated.
func readHopList(b []byte) ([]uint64, []byte, error) {
	count, n, err := ReadVarint(b)
	if err != nil {
		return nil, b, err
	}
	b = b[n:]

	if count > uint64(len(b)) {
		return nil, b, ErrMessageTooShort
	}
	ids := make([]uint64, 0, count)
	for range count {
		id, n, err := ReadVarint(b)
		if err != nil {
			return nil, b, err
		}
		ids = append(ids, id)
		b = b[n:]
	}
	return ids, b, nil
}

func readRouteCosts(b []byte) (warm, cold uint64, rest []byte, err error) {
	warm, n, err := ReadVarint(b)
	if err != nil {
		return 0, 0, b, err
	}
	b = b[n:]
	cold, n, err = ReadVarint(b)
	if err != nil {
		return 0, 0, b, err
	}
	return warm, cold, b[n:], nil
}
