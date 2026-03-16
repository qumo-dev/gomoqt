package webtransportgo

import (
	"context"
	"crypto/tls"
	"net/http"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/okdaichi/gomoqt/quic"
)

func Dial(ctx context.Context, addr string, header http.Header, tlsConfig *tls.Config) (*http.Response, quic.Connection, error) {
	d := quicgo_webtransportgo.Dialer{
		TLSClientConfig:      tlsConfig,
		ApplicationProtocols: []string{"moq-lite-03"},
	}
	rsp, wtsess, err := d.Dial(ctx, addr, header)

	return rsp, wrapSession(wtsess), err
}
