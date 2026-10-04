package webtransportgo

import (
	"context"
	"testing"

	webtransport "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCloseCause_PeerClose(t *testing.T) {
	cause := sessionCloseCause(&webtransport.SessionError{
		ErrorCode: 2,
		Message:   "expired",
		Remote:    true,
	}, context.Canceled)

	var appErr *transport.ApplicationError
	require.ErrorAs(t, cause, &appErr)
	assert.Equal(t, transport.ApplicationErrorCode(2), appErr.ErrorCode)
	assert.Equal(t, "expired", appErr.ErrorMessage)
	assert.True(t, appErr.Remote)
}

func TestSessionCloseCause_Fallback(t *testing.T) {
	cause := sessionCloseCause(context.Canceled, context.Canceled)
	assert.ErrorIs(t, cause, context.Canceled)
}
