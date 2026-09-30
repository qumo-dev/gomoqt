package moqt

import (
	"context"
	"errors"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
)

func TestCause(t *testing.T) {
	tests := map[string]struct {
		setupCtx func() context.Context
		expected error
	}{
		"no cause": {
			setupCtx: func() context.Context {
				return context.Background()
			},
			expected: nil,
		},
		"with cancel cause": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				cancel(errors.New("test error"))
				return ctx
			},
			expected: errors.New("test error"),
		},
		"with stream error": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 1, Remote: true}
				cancel(streamErr)
				return ctx
			},
			expected: &transport.StreamError{StreamID: 1, ErrorCode: 1, Remote: true},
		},
		"with stream error and announce stream type": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 2, Remote: true}
				ctx = context.WithValue(ctx, biStreamTypeCtxKey, message.StreamTypeAnnounce)
				cancel(streamErr)
				return ctx
			},
			expected: &AnnounceError{
				StreamError: &transport.StreamError{StreamID: 1, ErrorCode: 2, Remote: true},
			},
		},
		"with stream error and subscribe stream type": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 3, Remote: true}
				ctx = context.WithValue(ctx, biStreamTypeCtxKey, message.StreamTypeSubscribe)
				cancel(streamErr)
				return ctx
			},
			expected: &SubscribeError{
				StreamError: &transport.StreamError{StreamID: 1, ErrorCode: 3, Remote: true},
			},
		},
		"with stream error and fetch stream type": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 4, Remote: true}
				ctx = context.WithValue(ctx, biStreamTypeCtxKey, message.StreamTypeFetch)
				cancel(streamErr)
				return ctx
			},
			expected: &FetchError{
				StreamError: &transport.StreamError{StreamID: 1, ErrorCode: 4, Remote: true},
			},
		},
		"with stream error and probe stream type": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 5, Remote: true}
				ctx = context.WithValue(ctx, biStreamTypeCtxKey, message.StreamTypeProbe)
				cancel(streamErr)
				return ctx
			},
			expected: &ProbeError{
				StreamError: &transport.StreamError{StreamID: 1, ErrorCode: 5, Remote: true},
			},
		},
		"with stream error and group stream type": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				streamErr := &transport.StreamError{StreamID: 1, ErrorCode: 6, Remote: true}
				ctx = context.WithValue(ctx, uniStreamTypeCtxKey, message.StreamTypeGroup)
				cancel(streamErr)
				return ctx
			},
			expected: &GroupError{
				StreamError: &transport.StreamError{StreamID: 1, ErrorCode: 6, Remote: true},
			},
		},
		"with application error": {
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancelCause(context.Background())
				appErr := &transport.ApplicationError{Remote: false, ErrorCode: 5, ErrorMessage: "app error"}
				cancel(appErr)
				return ctx
			},
			expected: &SessionError{
				ApplicationError: &transport.ApplicationError{Remote: false, ErrorCode: 5, ErrorMessage: "app error"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := tt.setupCtx()
			err := Cause(ctx)
			if tt.expected == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Equal(t, tt.expected, err)
			}
		})
	}
}
