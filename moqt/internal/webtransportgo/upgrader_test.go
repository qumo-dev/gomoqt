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

func TestUpgrader_Upgrade_ProtocolMismatch(t *testing.T) {
	u := &Upgrader{
		ApplicationProtocols: []string{"moq-lite-05"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("CONNECT", "https://example.com/moq", nil)
	r.Header.Set("WT-Available-Protocols", `"moq-lite-04"`)

	conn, err := u.Upgrade(w, r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no supported application protocol")
	assert.Nil(t, conn)
}

func TestUpgrader_hasMatchingProtocol(t *testing.T) {
	t.Run("empty application protocols", func(t *testing.T) {
		u := &Upgrader{}
		assert.False(t, u.hasMatchingProtocol([]string{`"moq-lite-05"`}))
	})

	t.Run("invalid structured header syntax", func(t *testing.T) {
		u := &Upgrader{ApplicationProtocols: []string{"moq-lite-05"}}
		assert.False(t, u.hasMatchingProtocol([]string{`invalid,,,///`}))
	})

	t.Run("non-string items in list", func(t *testing.T) {
		u := &Upgrader{ApplicationProtocols: []string{"moq-lite-05"}}
		assert.False(t, u.hasMatchingProtocol([]string{`12345`}))
	})

	t.Run("matching protocol found", func(t *testing.T) {
		u := &Upgrader{ApplicationProtocols: []string{"moq-lite-05"}}
		assert.True(t, u.hasMatchingProtocol([]string{`"moq-lite-04", "moq-lite-05"`}))
	})
}

