package quicgo

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/okdaichi/gomoqt/transport"
	"github.com/quic-go/quic-go"
	quicgo_quicgo "github.com/quic-go/quic-go"
)

// var _ quic.ListenAddrFunc = ListenAddrEarly

func ListenAddrEarly(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (transport.QUICListener, error) {
	ln, err := quicgo_quicgo.ListenAddrEarly(addr, tlsConfig, quicConfig)
	return wrapListener(ln), err
}

// var _ quic.Listener = (*listenerWrapper)(nil)

func wrapListener(quicListener *quicgo_quicgo.EarlyListener) transport.QUICListener {
	return &listenerWrapper{
		listener: quicListener,
	}
}

type listenerWrapper struct {
	listener *quicgo_quicgo.EarlyListener
}

func (wrapper *listenerWrapper) Accept(ctx context.Context) (transport.StreamConn, error) {
	conn, err := wrapper.listener.Accept(ctx)
	return wrapConnection(conn), err
}

func (wrapper *listenerWrapper) Addr() net.Addr {
	return wrapper.listener.Addr()
}

func (wrapper *listenerWrapper) Close() error {
	return wrapper.listener.Close()
}
