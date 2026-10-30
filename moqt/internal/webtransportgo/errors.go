package webtransportgo

import (
	"context"
	"errors"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// moqt reads stream and session errors as transport errors, which are
// quic-go's. webtransport-go reports them as its own types, with the
// application's error code already mapped back from the HTTP/3 code space.
// The functions here turn them into what native QUIC would report.

// streamError returns err as a *transport.StreamError with the application's
// code if it is a webtransport-go stream reset, and err itself otherwise.
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

// streamCause returns cause, the context cause of stream id, as native QUIC
// would report it: a stream reset as a *transport.StreamError, and the end of
// the session as a *transport.ApplicationError. Any other cause, such as a
// local close, is returned unchanged.
func streamCause(id quic.StreamID, cause error) error {
	return sessionError(streamError(id, cause))
}

// streamContext returns a context with parent's values that ends when parent
// does, with parent's cause passed through streamCause. If parent has already
// ended, so has the context returned.
func streamContext(parent context.Context, id quic.StreamID) context.Context {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	if parent.Err() != nil {
		cancel(streamCause(id, context.Cause(parent)))
		return ctx
	}
	context.AfterFunc(parent, func() { cancel(streamCause(id, context.Cause(parent))) })
	return ctx
}
