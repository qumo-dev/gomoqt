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

// withValuesOf returns lifetime with the values of values as a fallback: its
// deadline, cancellation and cause are lifetime's alone, and Value consults
// lifetime first, then values. It is how a handler's context sees what
// [Server.ConnContext] stored for its connection, the way net/http derives
// each request's context from its connection's, while still ending with its
// own stream.
//
// The context package has no way to merge two contexts; this is the same
// technique context.WithoutCancel uses internally, in reverse: a wrapper
// that overrides only Value. Nothing runs in the background, so the result
// is done exactly when lifetime is, and context.Cause finds lifetime's cause
// (it looks the cancel context up through Value, which lifetime answers first).
func withValuesOf(lifetime, values context.Context) context.Context {
	return valuesCtx{Context: lifetime, values: values}
}

type valuesCtx struct {
	context.Context
	values context.Context
}

func (c valuesCtx) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
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
