package quicgo

import (
	"context"
	"crypto/tls"

	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
	quicgo_quicgo "github.com/quic-go/quic-go"
)

func DialAddrEarly(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.StreamConn, error) {
	conn, err := quicgo_quicgo.DialAddrEarly(ctx, addr, tlsConfig, quicConfig)

	return wrapConnection(conn), err
}
