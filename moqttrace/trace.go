package moqttrace

import (
	"context"
)

// SessionTrace is a set of hooks to run at various stages of an MoQ session.
// Any particular hook may be nil. Functions may be called concurrently from
// different goroutines and some may be called after the session has ended.
type SessionTrace struct {
	// SubscribeSent is called when a SUBSCRIBE message is about to be sent.
	SubscribeSent func(info SubscribeRequestInfo)

	// SubscribeSentError is called when sending a SUBSCRIBE message fails.
	SubscribeSentError func(info SubscribeRequestInfo, err error)

	// SubscribeAccepted is called when a SUBSCRIBE_OK message is received.
	SubscribeAccepted func(info SubscribeInfo)

	// SubscribeAcceptedError is called when an error occurs while processing
	// a SUBSCRIBE response (e.g., protocol error or timeout).
	SubscribeAcceptedError func(info SubscribeInfo, err error)

	// SubscribeDrop is called when a SUBSCRIBE_DROP message is received.
	SubscribeDrop func(info SubscribeDropInfo)

	// FetchSent is called when a FETCH message is about to be sent.
	FetchSent func(info FetchRequestInfo)

	// FetchSentError is called when sending a FETCH message fails.
	FetchSentError func(info FetchRequestInfo, err error)

	// SessionClosed is called when the session is closed.
	SessionClosed func(info SessionCloseInfo)

	// GoawayReceived is called when a GOAWAY message is received.
	GoawayReceived func(newSessionURI string)

	// ProbeResult is called when a PROBE result is available.
	ProbeResult func(info ProbeResultInfo)
}

// SubscribeRequestInfo contains information about a SUBSCRIBE request.
type SubscribeRequestInfo struct {
	SubscribeID uint64
	Path        string
	Name        string
	Priority    uint8
	Ordered     bool
	MaxLatency  uint64
	StartGroup  uint64
	EndGroup    uint64
}

// SubscribeInfo contains information about an accepted subscription.
type SubscribeInfo struct {
	SubscribeID uint64
	Priority    uint8
	Ordered     bool
	MaxLatency  uint64
	StartGroup  uint64
	EndGroup    uint64
}

// SubscribeDropInfo contains information about a subscription drop.
type SubscribeDropInfo struct {
	SubscribeID uint64
	StartGroup  uint64
	EndGroup    uint64
	ErrorCode   uint64
}

// FetchRequestInfo contains information about a FETCH request.
type FetchRequestInfo struct {
	Path          string
	Name          string
	Priority      uint8
	GroupSequence uint64
}

// SessionCloseInfo contains information about a session closure.
type SessionCloseInfo struct {
	ErrorCode    uint64
	ErrorMessage string
}

// ProbeResultInfo contains information about a probe result.
type ProbeResultInfo struct {
	Bitrate uint64
	RTT     uint64 // in milliseconds
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
