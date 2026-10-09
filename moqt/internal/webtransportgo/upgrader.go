package webtransportgo

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/dunglas/httpsfv"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/transport"
)

const wtAvailableProtocolsHeader = "WT-Available-Protocols"

type Upgrader struct {
	CheckOrigin          func(r *http.Request) bool
	ApplicationProtocols []string
	ReorderingTimeout    time.Duration
}

func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (transport.WebTransportSession, error) {
	// If the client explicitly offered application protocols via WT-Available-Protocols,
	// ensure that at least one offered protocol is supported by this upgrader.
	// Per draft-ietf-webtrans-http3-15 Section 3.3:
	// "If none of the protocols in WT-Available-Protocols are supported, the server
	// MUST reject the WebTransport session by returning an error status code".
	offered := r.Header[http.CanonicalHeaderKey(wtAvailableProtocolsHeader)]
	if len(offered) > 0 && !u.hasMatchingProtocol(offered) {
		return nil, errors.New("webtransport: no supported application protocol")
	}

	s := quicgo_webtransportgo.Upgrader{
		CheckOrigin:          u.CheckOrigin,
		ApplicationProtocols: u.ApplicationProtocols,
		ReorderingTimeout:    u.ReorderingTimeout,
	}
	sess, err := s.Upgrade(w, r)
	return wrapSession(sess), err
}

func (u *Upgrader) hasMatchingProtocol(theirs []string) bool {
	if len(u.ApplicationProtocols) == 0 {
		return false
	}
	list, err := httpsfv.UnmarshalList(theirs)
	if err != nil {
		return false
	}
	for _, item := range list {
		if i, ok := item.(httpsfv.Item); ok {
			if protocol, ok := i.Value.(string); ok {
				if slices.Contains(u.ApplicationProtocols, protocol) {
					return true
				}
			}
		}
	}
	return false
}
