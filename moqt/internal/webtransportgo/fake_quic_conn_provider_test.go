package webtransportgo

import (
	quicgo "github.com/quic-go/quic-go"
)

var _ quicConnProvider = (*fakeQUICConnProvider)(nil)

// fakeQUICConnProvider implements both transport.StreamConn and quicConnProvider
// to test the ServeQUICConn happy path.
type fakeQUICConnProvider struct {
	fakeStreamConn
	qc *quicgo.Conn
}

func (f *fakeQUICConnProvider) QUICConn() *quicgo.Conn {
	return f.qc
}
