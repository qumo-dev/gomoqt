package webtransportgo

import (
	"context"
	"crypto/tls"
	"net/http"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

type Dialer struct {
	TLSClientConfig      *tls.Config
	ApplicationProtocols []string
}

func (d *Dialer) Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, transport.StreamConn, error) {
	dialer := quicgo_webtransportgo.Dialer{
		TLSClientConfig:      tlsConfig,
		ApplicationProtocols: []string{"moq-lite-03"},
	}
	rsp, wtsess, err := dialer.Dial(ctx, addr, header)

	return rsp, wrapSession(wtsess), err
}
