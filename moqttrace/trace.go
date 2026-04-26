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

// SubscribeDropInfo contains information about a subscription drop.
type SubscribeDropInfo struct {
	SubscribeID uint64
	StartGroup  uint64
	EndGroup    uint64
	ErrorCode   uint64
}

// ProbeResultInfo contains information about a probe result.
type ProbeResultInfo struct {
	Bitrate uint64
}

type traceContextKey struct{}

// ContextWithSessionTrace returns a new context based on the provided parent
// context that carries the given SessionTrace.
func ContextWithSessionTrace(ctx context.Context, trace *SessionTrace) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// SessionTraceFromContext returns the SessionTrace associated with the
// provided context, or nil if none is present.
func SessionTraceFromContext(ctx context.Context) *SessionTrace {
	if trace, ok := ctx.Value(traceContextKey{}).(*SessionTrace); ok {
		return trace
	}
	return nil
}
