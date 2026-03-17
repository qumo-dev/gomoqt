package moqt

import (
	"context"
	"errors"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/transport"
)

// Cause translates a Go context cancellation reason into a package-specific error type.
// When the provided context was canceled because of a QUIC stream error or application error,
// Cause converts that into the corresponding moqt error (e.g., SessionError, AnnounceError,
// SubscribeError, GroupError).
// If no specific translation is available, the original context cause is returned unchanged.
func Cause(ctx context.Context) error {
	reason := context.Cause(ctx)

	var strErr *transport.StreamError
	if errors.As(reason, &strErr) {
		st, ok := ctx.Value(&biStreamTypeCtxKey).(message.StreamType)
		if ok {
			switch st {
			case message.StreamTypeSession:
				// The underlying QUIC or WebTransport stream may carry a
				// stream-level error code which should not be reinterpreted
				// as an application-level error code. Some transports (e.g.
				// WebTransport) limit the stream error to 32 bits and may
				// map the value on the QUIC wire in a way that would make
				// casting it to an ApplicationErrorCode invalid or out of
				// range. To avoid this, translate a session stream reset to
				// a generic session-level application error instead of
				// reusing the stream's numeric value.
				return &SessionError{
					ApplicationError: &transport.ApplicationError{
						Remote:       strErr.Remote,
						ErrorCode:    transport.ApplicationErrorCode(ProtocolViolationErrorCode),
						ErrorMessage: "moqt: closed session stream",
					},
				}
			case message.StreamTypeAnnounce:
				return &AnnounceError{
					StreamError: strErr,
				}
			case message.StreamTypeSubscribe:
				return &SubscribeError{
					StreamError: strErr,
				}
			}

			return reason
		}

		st, ok = ctx.Value(&uniStreamTypeCtxKey).(message.StreamType)
		if ok {
			switch st {
			case message.StreamTypeGroup:
				return &GroupError{
					StreamError: strErr,
				}
			}
		}

		return reason
	}

	var appErr *transport.ApplicationError
	if errors.As(reason, &appErr) {
		return &SessionError{
			ApplicationError: appErr,
		}
	}

	return reason
}

var biStreamTypeCtxKey int
var uniStreamTypeCtxKey int
