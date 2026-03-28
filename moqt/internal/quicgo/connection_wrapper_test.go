package quicgo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapConnection_Nil(t *testing.T) {
	assert.Nil(t, wrapConnection(nil))
}

func TestWrapConnectionExported_Nil(t *testing.T) {
	assert.Nil(t, WrapConnection(nil))
}

func TestConnWrapper_Unwrap(t *testing.T) {
	w := connWrapper{}
	assert.Nil(t, w.Unwrap())
}
