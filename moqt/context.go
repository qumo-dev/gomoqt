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
// SubscribeError, GroupError). A WebTransport stream reset or session close error is
// translated the same way, with the same code and Remote flag as over native QUIC; a stream
// error from WebTransport carries no stream ID.
// If no specific translation is available, the original context cause is returned unchanged.
func Cause(ctx context.Context) error {
	reason := context.Cause(ctx)

	// A WebTransport stream's context ends with webtransport-go's reset error,
	// which carries the application's code: read it as native QUIC's.
	if wtErr, ok := errors.AsType[*webtransport.StreamError](reason); ok {
		reason = &transport.StreamError{
			ErrorCode: transport.StreamErrorCode(wtErr.ErrorCode),
			Remote:    wtErr.Remote,
		}
	}

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
