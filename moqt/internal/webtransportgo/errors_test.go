package webtransportgo

import (
	"errors"
	"fmt"
	"testing"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
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
