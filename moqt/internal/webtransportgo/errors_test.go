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

// httpCode maps a WebTransport stream error code into the HTTP/3 code space,
// as a peer puts it on the wire.
func httpCode(n uint32) quic.StreamErrorCode {
	return firstHTTPCode + quic.StreamErrorCode(n) + quic.StreamErrorCode(n/0x1e)
}

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
	sessionGone := &quic.StreamError{StreamID: 4, ErrorCode: quicgo_webtransportgo.WTSessionGoneErrorCode, Remote: true}
	reserved := &quic.StreamError{StreamID: 4, ErrorCode: firstHTTPCode + 0x1e, Remote: true}
	tests := map[string]struct {
		cause error
		want  error
	}{
		"code 0": {
			cause: &quic.StreamError{StreamID: 4, ErrorCode: httpCode(0), Remote: true},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 0, Remote: true},
		},
		"code 3": {
			cause: &quic.StreamError{StreamID: 4, ErrorCode: httpCode(3), Remote: true},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true},
		},
		"code past a reserved code point": {
			cause: &quic.StreamError{StreamID: 4, ErrorCode: httpCode(0x1e)},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 0x1e},
		},
		"largest code": {
			cause: &quic.StreamError{StreamID: 4, ErrorCode: httpCode(0xffffffff)},
			want:  &transport.StreamError{StreamID: 4, ErrorCode: 0xffffffff},
		},
		"code outside the range": {cause: sessionGone, want: sessionGone},
		"reserved code point":    {cause: reserved, want: reserved},
		"context canceled":       {cause: context.Canceled, want: context.Canceled},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, streamCause(tt.cause))
		})
	}
}

type streamContextKey struct{}

func TestStreamContext(t *testing.T) {
	parent, cancel := context.WithCancelCause(context.WithValue(t.Context(), streamContextKey{}, "value"))
	ctx := streamContext(parent)
	assert.Equal(t, "value", ctx.Value(streamContextKey{}))
	require.NoError(t, ctx.Err())

	cancel(&quic.StreamError{StreamID: 4, ErrorCode: httpCode(3), Remote: true})

	<-ctx.Done()
	assert.Equal(t, &transport.StreamError{StreamID: 4, ErrorCode: 3, Remote: true}, context.Cause(ctx))
}
