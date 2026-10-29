package message

import (
	"io"
)

// SubscribeRedirectMessage tells the subscriber that the publisher will not
// serve the subscription, and where to subscribe instead. It is sent in
// place of SUBSCRIBE_OK, as the only message on the Subscribe Stream, and
// only to a peer that advertised the Redirect Setup Parameter.
//
// Wire format:
//
//	SUBSCRIBE_REDIRECT Message {
//	  Type (i) = 0x3
//	  Message Length (i)
//	  Connect URI (s)
//	  Broadcast Path (s)
//	  Track Name (s)
//	}
type SubscribeRedirectMessage struct {
	// ConnectURI is the session to subscribe on; empty means the current one.
	ConnectURI string
	// BroadcastPath and TrackName name the track to subscribe to instead.
	BroadcastPath string
	TrackName     string
}

func (m SubscribeRedirectMessage) Len() int {
	return StringLen(m.ConnectURI) +
		StringLen(m.BroadcastPath) +
		StringLen(m.TrackName)
}

func (m SubscribeRedirectMessage) Encode(w io.Writer) error {
	msgLen := m.Len()
	b := make([]byte, 0, msgLen+VarintLen(uint64(msgLen)))

	b, _ = WriteMessageLength(b, uint64(msgLen))
	b, _ = WriteString(b, m.ConnectURI)
	b, _ = WriteString(b, m.BroadcastPath)
	b, _ = WriteString(b, m.TrackName)

	_, err := w.Write(b)
	return err
}

func (m *SubscribeRedirectMessage) Decode(src io.Reader) error {
	b, err := readBody(src)
	if err != nil {
		return err
	}

	uri, n, err := ReadString(b)
	if err != nil {
		return err
	}
	b = b[n:]

	path, n, err := ReadString(b)
	if err != nil {
		return err
	}
	b = b[n:]

	name, n, err := ReadString(b)
	if err != nil {
		return err
	}
	b = b[n:]

	if len(b) != 0 {
		return ErrMessageTooShort
	}

	m.ConnectURI = uri
	m.BroadcastPath = path
	m.TrackName = name
	return nil
}
