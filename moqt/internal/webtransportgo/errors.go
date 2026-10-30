package webtransportgo

import (
	"errors"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// streamError returns err as the *transport.StreamError native QUIC reports
// for stream id if it is a webtransport-go stream reset, and err itself
// otherwise. moqt matches resets by the transport type, so without it a
// reset's error code is lost.
func streamError(id quic.StreamID, err error) error {
	if wtErr, ok := errors.AsType[*quicgo_webtransportgo.StreamError](err); ok {
		return &transport.StreamError{
			StreamID:  id,
			ErrorCode: transport.StreamErrorCode(wtErr.ErrorCode),
			Remote:    wtErr.Remote,
		}
	}
	return err
}
