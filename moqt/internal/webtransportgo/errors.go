package webtransportgo

import (
	"errors"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// moqt matches stream and session errors as transport errors, which are
// quic-go's types. webtransport-go reports them as its own types, already
// carrying the application's error code. The functions here turn them into
// what native QUIC would report.

// streamError returns err as a *transport.StreamError for stream id if it is a
// webtransport-go stream reset, and err itself otherwise.
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

// sessionError returns err as a *transport.ApplicationError if it is a
// webtransport-go session close, and err itself otherwise.
func sessionError(err error) error {
	if wtErr, ok := errors.AsType[*quicgo_webtransportgo.SessionError](err); ok {
		return &transport.ApplicationError{
			ErrorCode:    transport.ApplicationErrorCode(wtErr.ErrorCode),
			ErrorMessage: wtErr.Message,
			Remote:       wtErr.Remote,
		}
	}
	return err
}
