package webtransportgo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialer_Dial_InvalidAddress(t *testing.T) {
	d := &Dialer{}
	ctx := t.Context()

	rsp, conn, err := d.Dial(ctx, "://bad-url", nil, nil)

	require.Error(t, err)
	assert.Nil(t, rsp)
	// Current implementation always wraps the returned WT session value,
	// even on error where the session is nil.
	assert.NotNil(t, conn)
}
