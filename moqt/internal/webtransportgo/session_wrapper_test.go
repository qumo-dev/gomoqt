package webtransportgo

import (
	"context"
	"fmt"
	"testing"

	webtransport "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloseCause_PeerClose(t *testing.T) {
	cause := CloseCause(&webtransport.SessionError{
		ErrorCode: 2,
		Message:   "expired",
		Remote:    true,
	})

	var appErr *transport.ApplicationError
	require.ErrorAs(t, cause, &appErr)
	assert.Equal(t, transport.ApplicationErrorCode(2), appErr.ErrorCode)
	assert.Equal(t, "expired", appErr.ErrorMessage)
	assert.True(t, appErr.Remote)
}

func TestCloseCause_Wrapped(t *testing.T) {
	cause := CloseCause(fmt.Errorf("closed: %w", &webtransport.SessionError{ErrorCode: 2, Message: "refused"}))

	var appErr *transport.ApplicationError
	require.ErrorAs(t, cause, &appErr)
	assert.Equal(t, "refused", appErr.ErrorMessage)
	assert.False(t, appErr.Remote)
}

func TestCloseCause_OtherCause(t *testing.T) {
	cause := CloseCause(context.Canceled)

	assert.ErrorIs(t, cause, context.Canceled)
}
