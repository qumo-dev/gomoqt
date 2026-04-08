package quicgo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapConnection_Nil(t *testing.T) {
	assert.Nil(t, wrapConnection(nil))
}
