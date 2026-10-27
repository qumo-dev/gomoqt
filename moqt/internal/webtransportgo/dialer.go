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
// The QUIC connection is dialed through [quicgo.Dial], so the session keeps
// through a network change, and moves between paths, as a native QUIC one
// does.
func Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config, quicConfig *quic.Config, appProtocols []string) (*http.Response, transport.WebTransportSession, error) {
	var qconn *quic.Conn
	tr := quicgo_webtransportgo.Transport{
		TLSClientConfig:      tlsConfig,
		QUICConfig:           quicConfig,
		ApplicationProtocols: appProtocols,
		// Called once, before Dial returns.
		DialAddr: func(ctx context.Context, addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
			conn, err := quicgo.Dial(ctx, addr, tlsConfig, quicConfig)
			if err != nil {
				return nil, err
			}
			qconn = conn
			return conn, nil
		},
	}
	rsp, wtsess, err := tr.Dial(ctx, addr, header)
	if wtsess == nil {
		return rsp, nil, err
	}
	return rsp, &sessionWrapper{sess: wtsess, qconn: qconn}, err
}
