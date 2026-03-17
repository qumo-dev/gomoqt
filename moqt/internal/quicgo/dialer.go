package quicgo

import (
	"context"
	"crypto/tls"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_quicgo "github.com/quic-go/quic-go"
)

func DialAddrEarly(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *transport.QUICConfig) (transport.StreamConn, error) {
	conn, err := quicgo_quicgo.DialAddrEarly(ctx, addr, tlsConfig, quicConfig)

	return wrapConnection(conn), err
}
