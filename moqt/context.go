package moqt

import (
	"context"
	"errors"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

type biStreamTypeCtxKeyType struct{}
type uniStreamTypeCtxKeyType struct{}

var biStreamTypeCtxKey biStreamTypeCtxKeyType = biStreamTypeCtxKeyType{}
var uniStreamTypeCtxKey uniStreamTypeCtxKeyType = uniStreamTypeCtxKeyType{}

type sessionCtxKeyType struct{}

var sessionCtxKey sessionCtxKeyType = sessionCtxKeyType{}

// SessionFromContext returns the session a peer-initiated request arrived on.
// The context of a [TrackWriter] handed to a [TrackHandler], and of a
// [FetchRequest] handed to a [FetchHandler], carries it, so a handler serving
// many sessions from one [TrackMux] can tell who is asking — for example to
// authorize a SUBSCRIBE per session.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(sessionCtxKey).(*Session)
	return sess, ok
}

// withSession returns ctx carrying sess for [SessionFromContext].
func withSession(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, sess)
}

// Cause translates a Go context cancellation reason into a package-specific error type.
// When the provided context was canceled because of a QUIC stream error or application error,
// Cause converts that into the corresponding moqt error (e.g., SessionError, AnnounceError,
// SubscribeError, GroupError).
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

	if appErr, ok := errors.AsType[*transport.ApplicationError](reason); ok {
		return &SessionError{
			ApplicationError: appErr,
		}
	}

	return reason
}
