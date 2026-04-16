package webtransportgo

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

func Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config, appProtocols []string) (*http.Response, transport.WebTransportSession, error) {
	dialer := quicgo_webtransportgo.Dialer{
		TLSClientConfig:      tlsConfig,
		ApplicationProtocols: appProtocols,
	}
	rsp, wtsess, err := dialer.Dial(ctx, addr, header)

	return rsp, wrapSession(wtsess), err
}
