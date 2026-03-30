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
	assert.Nil(t, conn)
}

func TestWrapSession_NilSession(t *testing.T) {
	conn := wrapSession(nil)
	assert.Nil(t, conn)
}
