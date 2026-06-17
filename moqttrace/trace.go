package moqttrace

import (
	"context"
)

// SessionTrace is a set of hooks to run at various stages of an MoQ sessionTrace.
// Any particular hook may be nil. Functions may be called concurrently from
// different goroutines and some may be called after the sessionTrace has ended.
type SessionTrace struct {
	// IncomingBiStreamStart is called when an incoming bidirectional stream
	// carries an unknown or unsupported stream type.
	IncomingBiStreamStart func(msgType uint8)

	// IncomingUniStreamStart is called when an incoming unidirectional stream
	// carries an unknown or unsupported stream type.
	IncomingUniStreamStart func(msgType uint8)
}

type traceContextKeyType struct{}

var traceContextKey traceContextKeyType

// ContextWithSessionTrace returns a new context based on the provided parent
// context that carries the given SessionTrace.
func ContextWithSessionTrace(ctx context.Context, trace *SessionTrace) context.Context {
	return context.WithValue(ctx, traceContextKey, trace)
}

// SessionTraceFromContext returns the SessionTrace associated with the
// provided context, or nil if none is present.
func SessionTraceFromContext(ctx context.Context) *SessionTrace {
	if trace, ok := ctx.Value(traceContextKey).(*SessionTrace); ok {
		return trace
	}
	return nil
}
