package webtransportgo

import (
	"context"
	"errors"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
)

// moqt reads stream and session errors as transport errors, which are
// quic-go's. webtransport-go reports them as its own types, and a reset's code
// on the wire is the WebTransport code mapped into the HTTP/3 code space. The
// functions here turn both back into what native QUIC would report.

// The range of HTTP/3 error codes WebTransport maps its stream error codes to.
const (
	firstHTTPCode quic.StreamErrorCode = 0x52e4a40fa8db
	lastHTTPCode  quic.StreamErrorCode = 0x52e5ac983162
)

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

// streamCause returns cause, a stream context's, with a stream reset's code
// mapped back from the HTTP/3 code space to the application's. Any other cause,
// a reset with a code outside the WebTransport range included, is returned
// unchanged.
func streamCause(cause error) error {
	strErr, ok := errors.AsType[*quic.StreamError](cause)
	if !ok {
		return cause
	}
	code, ok := webTransportCode(strErr.ErrorCode)
	if !ok {
		return cause
	}
	return &transport.StreamError{StreamID: strErr.StreamID, ErrorCode: code, Remote: strErr.Remote}
}

// webTransportCode maps an HTTP/3 error code back to the WebTransport stream
// error code it carries, as webtransport-go does for Read and Write. It
// reports false for a code outside the range or on a reserved code point.
func webTransportCode(h quic.StreamErrorCode) (transport.StreamErrorCode, bool) {
	if h < firstHTTPCode || h > lastHTTPCode || (h-0x21)%0x1f == 0 {
		return 0, false
	}
	shifted := h - firstHTTPCode
	return shifted - shifted/0x1f, true
}

// streamContext returns a context with parent's values that ends when parent
// does, with parent's cause passed through streamCause.
func streamContext(parent context.Context) context.Context {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	context.AfterFunc(parent, func() { cancel(streamCause(context.Cause(parent))) })
	return ctx
}
