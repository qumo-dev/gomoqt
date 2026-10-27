package transport

import (
	quicgo "github.com/quic-go/quic-go"
)

// QUICConfig configures a QUIC connection, as quic-go's Config does.
type QUICConfig = quicgo.Config

// QUICTransport is one UDP socket a QUIC connection can be dialed over and
// move onto, as quic-go's Transport is.
type QUICTransport = quicgo.Transport

// QUICPath is a network path of a QUIC connection, over one QUICTransport,
// as quic-go's Path is: probe it, switch the connection onto it, close it.
type QUICPath = quicgo.Path
