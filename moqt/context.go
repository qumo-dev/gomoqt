package moqt

import (
	"context"
	"errors"

	webtransport "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

type biStreamTypeCtxKeyType struct{}
type uniStreamTypeCtxKeyType struct{}

var biStreamTypeCtxKey biStreamTypeCtxKeyType = biStreamTypeCtxKeyType{}
var uniStreamTypeCtxKey uniStreamTypeCtxKeyType = uniStreamTypeCtxKeyType{}

// Cause translates a Go context cancellation reason into a package-specific error type.
// When the provided context was canceled because of a QUIC stream error or application error,
// Cause converts that into the corresponding moqt error (e.g., SessionError, AnnounceError,
// SubscribeError, GroupError). A WebTransport session's close error becomes a SessionError
// too, with the same code, message and Remote flag as over native QUIC.
// If no specific translation is available, the original context cause is returned unchanged.
func Cause(ctx context.Context) error {
	reason := context.Cause(ctx)

	if strErr, ok := errors.AsType[*transport.StreamError](reason); ok {
		st, ok := ctx.Value(biStreamTypeCtxKey).(message.StreamType)
		if ok {
			switch st {
			case message.StreamTypeAnnounce:
				return &AnnounceError{
					StreamError: strErr,
				}
			case message.StreamTypeSubscribe:
				return &SubscribeError{
					StreamError: strErr,
				}
			case message.StreamTypeFetch:
				return &FetchError{
					StreamError: strErr,
				}
			case message.StreamTypeProbe:
				return &ProbeError{
					StreamError: strErr,
				}
			}

			return reason
		}

		st, ok = ctx.Value(uniStreamTypeCtxKey).(message.StreamType)
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

	if sessErr, ok := errors.AsType[*webtransport.SessionError](reason); ok {
		return &SessionError{
			ApplicationError: &transport.ApplicationError{
				ErrorCode:    transport.ApplicationErrorCode(sessErr.ErrorCode),
				ErrorMessage: sessErr.Message,
				Remote:       sessErr.Remote,
			},
		}
	}

	if appErr, ok := errors.AsType[*transport.ApplicationError](reason); ok {
		return &SessionError{
			ApplicationError: appErr,
		}
	}

	return reason
}
