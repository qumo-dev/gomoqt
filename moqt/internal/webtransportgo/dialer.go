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
// The QUIC connection is dialed through [quicgo.DialEarly], so the session
// keeps through a network change as a native QUIC one does.
func Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config, quicConfig *quic.Config, appProtocols []string) (*http.Response, transport.WebTransportSession, error) {
	tr := quicgo_webtransportgo.Transport{
		TLSClientConfig:      tlsConfig,
		QUICConfig:           quicConfig,
		ApplicationProtocols: appProtocols,
		DialAddr:             quicgo.DialEarly,
	}
	rsp, wtsess, err := tr.Dial(ctx, addr, header)

	return rsp, wrapSession(wtsess), err
}
