package message

import "io"

/*
 *	CONTRIBUTE_REQUEST Message {
 *	  Message Length (i)
 *	  Broadcast Path (s)
 *	  Track Name (s)
 *	}
 */

// ContributeRequestMessage is the first message on a Contribute Stream: a
// publisher asks the receiver to subscribe to one track of a broadcast the
// publisher does not announce. Its layout is that of TRACK.
type ContributeRequestMessage struct {
	BroadcastPath string
	TrackName     string
}

func (crm ContributeRequestMessage) Len() int {
	return TrackMessage(crm).Len()
}

func (crm ContributeRequestMessage) Encode(w io.Writer) error {
	return TrackMessage(crm).Encode(w)
}

func (crm *ContributeRequestMessage) Decode(src io.Reader) error {
	return (*TrackMessage)(crm).Decode(src)
}
