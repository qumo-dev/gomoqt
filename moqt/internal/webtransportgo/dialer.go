package webtransportgo

import (
	"context"
	"crypto/tls"
	"net/http"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/quicgo"
	"github.com/qumo-dev/gomoqt/transport"
)

// Dial opens a WebTransport session to addr. quicConfig configures the QUIC
// connection under it and is passed to the library as it is: nil is the
// library's defaults, and any other must have datagrams and stream reset with
// partial delivery on, or the library refuses it.
//
// The QUIC connection is dialed over qtr, the caller's, if not nil, as
// quic.Transport.DialEarly does; else through [quicgo.DialEarly], so the
// session keeps through a network change as a native QUIC one does.
func Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config, quicConfig *quic.Config, appProtocols []string, qtr *quic.Transport) (*http.Response, transport.WebTransportSession, error) {
	var qconn *quic.Conn
	tr := quicgo_webtransportgo.Transport{
		TLSClientConfig:      tlsConfig,
		QUICConfig:           quicConfig,
		ApplicationProtocols: appProtocols,
		// Called once, before Dial returns.
		DialAddr: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
			conn, err := dialQUIC(ctx, qtr, addr, tlsConfig, quicConfig)
			qconn = conn
			return conn, err
		},
	}
	rsp, wtsess, err := tr.Dial(ctx, addr, header)
	if wtsess == nil {
		return rsp, nil, err
	}
	return rsp, &sessionWrapper{sess: wtsess, qconn: qconn}, err
}

// dialQUIC dials the QUIC connection under a session, over qtr if not nil.
func dialQUIC(ctx context.Context, qtr *quic.Transport, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
	if qtr == nil {
		return quicgo.DialEarly(ctx, addr, tlsConfig, quicConfig)
	}
	conn, err := quicgo.DialTransportEarly(ctx, qtr, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}
	return conn.(interface{ QUICConn() *quic.Conn }).QUICConn(), nil
}
