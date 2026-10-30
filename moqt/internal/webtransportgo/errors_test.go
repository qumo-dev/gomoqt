package webtransportgo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/quic-go/quic-go"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamError(t *testing.T) {
	other := errors.New("other")
	tests := map[string]struct {
		err  error
		want error
	}{
		"remote reset": {
			err:  &quicgo_webtransportgo.StreamError{ErrorCode: 3, Remote: true},
			want: &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true},
		},
		"local reset": {
			err:  &quicgo_webtransportgo.StreamError{ErrorCode: 0xffffffff},
			want: &transport.StreamError{StreamID: 4, ErrorCode: 0xffffffff},
		},
		"other error": {err: other, want: other},
		"nil":         {err: nil, want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, streamError(4, tt.err))
		})
	}
}

func TestSessionError(t *testing.T) {
	other := errors.New("other")
	tests := map[string]struct {
		err  error
		want error
	}{
		"session close": {
			err:  &quicgo_webtransportgo.SessionError{ErrorCode: 2, Message: "expired", Remote: true},
			want: &transport.ApplicationError{ErrorCode: 2, ErrorMessage: "expired", Remote: true},
		},
		"wrapped session close": {
			err:  fmt.Errorf("open: %w", &quicgo_webtransportgo.SessionError{ErrorCode: 2, Message: "expired"}),
			want: &transport.ApplicationError{ErrorCode: 2, ErrorMessage: "expired"},
		},
		"other error": {err: other, want: other},
		"nil":         {err: nil, want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, sessionError(tt.err))
		})
	}
}

func TestStreamCause(t *testing.T) {
	sessionGone := &quic.StreamError{StreamID: 4, ErrorCode: quicgo_webtransportgo.WTSessionGoneErrorCode}
	tests := map[string]struct {
		cause error
		want  error
	}{
		"stream reset": {
			cause: &quicgo_webtransportgo.StreamError{ErrorCode: 3, Remote: true},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true},
		},
		"largest code": {
			cause: &quicgo_webtransportgo.StreamError{ErrorCode: 0xffffffff},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 0xffffffff},
		},
		"session closed": {
			cause: &quicgo_webtransportgo.SessionError{ErrorCode: 7, Message: "bye", Remote: true},
			want:  &transport.ApplicationError{ErrorCode: 7, ErrorMessage: "bye", Remote: true},
		},
		"code outside the WebTransport range": {cause: sessionGone, want: sessionGone},
		"context canceled":                    {cause: context.Canceled, want: context.Canceled},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, streamCause(4, tt.cause))
		})
	}
}

type streamContextKey struct{}

func TestStreamContext(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.WithValue(t.Context(), streamContextKey{}, "value"))
	ctx := streamContext(parent, 4)
	assert.Equal(t, "value", ctx.Value(streamContextKey{}))
	require.NoError(t, ctx.Err())

	cancel(&quicgo_webtransportgo.StreamError{ErrorCode: 3, Remote: true})

	<-ctx.Done()
	assert.Equal(t, &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true}, context.Cause(ctx))
}

func TestStreamContext_ParentAlreadyDone(t *testing.T) {
	parent, cancel := context.WithCancelCause(t.Context())
	cancel(&quicgo_webtransportgo.StreamError{ErrorCode: 3, Remote: true})

	ctx := streamContext(parent, 4)

	require.Error(t, ctx.Err(), "done right away, not on another goroutine")
	assert.Equal(t, &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true}, context.Cause(ctx))
}
