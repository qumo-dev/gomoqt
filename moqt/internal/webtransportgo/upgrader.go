package webtransportgo

import (
	"net/http"
	"time"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

type Upgrader struct {
	CheckOrigin          func(r *http.Request) bool
	ApplicationProtocols []string
	ReorderingTimeout    time.Duration
}

func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (transport.WebTransportSession, error) {
	s := quicgo_webtransportgo.Upgrader{
		CheckOrigin:          u.CheckOrigin,
		ApplicationProtocols: u.ApplicationProtocols,
		ReorderingTimeout:    u.ReorderingTimeout,
	}
	sess, err := s.Upgrade(w, r)
	return wrapSession(sess), err
}
