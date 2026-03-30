package webtransportgo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDial_InvalidAddress(t *testing.T) {
	rsp, conn, err := Dial(context.Background(), "://bad-url", nil, nil)

	require.Error(t, err)
	assert.Nil(t, rsp)
	assert.Nil(t, conn)
}
