package webtransportgo

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgrader_Upgrade_NonWebTransportRequest(t *testing.T) {
	u := &Upgrader{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://example.com/moq", nil)

	conn, err := u.Upgrade(w, r)

	require.Error(t, err)
	// Current implementation wraps session value regardless of upgrade failure.
	assert.NotNil(t, conn)
}

func TestWrapSession_NilSession(t *testing.T) {
	conn := wrapSession(nil)
	assert.NotNil(t, conn)

	wrapper, ok := conn.(*sessionWrapper)
	require.True(t, ok)
	assert.Nil(t, wrapper.sess)
}
