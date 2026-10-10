package moqt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnManager_AddSession(t *testing.T) {
	tests := map[string]struct {
		sessions  []StreamConn
		wantCount int
	}{
		"nil session is ignored": {
			sessions:  []StreamConn{nil},
			wantCount: 0,
		},
		"duplicate session is counted once": {
			sessions: func() []StreamConn {
				session := &FakeStreamConn{}
				return []StreamConn{session, session}
			}(),
			wantCount: 1,
		},
		"multiple distinct sessions are counted": {
			sessions:  []StreamConn{&FakeStreamConn{}, &FakeStreamConn{}},
			wantCount: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := newConnManager()
			for _, session := range tt.sessions {
				manager.addConn(session)
			}

			assert.Equal(t, tt.wantCount, manager.countSessions())
		})
	}
}

func TestConnManager_RemoveSession(t *testing.T) {
	manager := newConnManager()
	first := &FakeStreamConn{}
	second := &FakeStreamConn{}
	manager.addConn(first)
	manager.addConn(second)
	done := manager.Done()

	manager.removeConn(nil)
	assert.Equal(t, 2, manager.countSessions())

	manager.removeConn(first)
	assert.Equal(t, 1, manager.countSessions())

	select {
	case <-done:
		t.Fatal("done channel should not be closed while sessions remain")
	default:
	}

	manager.removeConn(second)
	assert.Equal(t, 0, manager.countSessions())

	select {
	case <-done:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel did not close after removing last session")
	}

	// Removing again should be a no-op.
	manager.removeConn(second)
	assert.Equal(t, 0, manager.countSessions())
}

func TestConnManager_Done(t *testing.T) {
	manager := newConnManager()
	first := manager.Done()

	select {
	case <-first:
		// expected: no sessions means we're already done
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel should be closed when there are no tracked sessions")
	}

	session := &FakeStreamConn{}
	manager.addConn(session)
	second := manager.Done()
	assert.NotEqual(t, first, second)

	select {
	case <-second:
		t.Fatal("done channel should not be closed while a session is active")
	default:
	}

	manager.removeConn(session)

	select {
	case <-second:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel should close once the tracked session count reaches zero")
	}
}

func TestConnManager_Close(t *testing.T) {
	tests := map[string]struct {
		setup   func() *connManager
		wantErr string
	}{
		"empty manager closes": {
			setup: func() *connManager {
				return newConnManager()
			},
		},
		"active sessions prevent close": {
			setup: func() *connManager {
				manager := newConnManager()
				manager.addConn(&FakeStreamConn{})
				return manager
			},
			wantErr: "cannot close session manager with active sessions",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := tt.setup()

			err := manager.Close()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}

	t.Run("close is idempotent", func(t *testing.T) {
		manager := newConnManager()
		require.NoError(t, manager.Close())
		require.NoError(t, manager.Close())
	})

	t.Run("closed manager ignores new sessions", func(t *testing.T) {
		manager := newConnManager()
		require.NoError(t, manager.Close())

		manager.addConn(&FakeStreamConn{})
		assert.Zero(t, manager.countSessions())
	})
}

// A shutdown ends every connection the manager had when it drained, and
// the manager takes none after: no connection is left that the shutdown
// neither ends nor is told to wait for.
func TestConnManager_drain(t *testing.T) {
	before, after := &FakeStreamConn{}, &FakeStreamConn{}
	manager := newConnManager()
	manager.addConn(before)

	drained := manager.drain()
	manager.addConn(after)

	assert.Equal(t, []StreamConn{before}, drained)
	assert.True(t, manager.tracks(before))
	assert.False(t, manager.tracks(after), "a connection that comes while draining is refused")
	assert.Equal(t, 1, manager.countSessions())

	// The drained connection still counts until it is removed.
	manager.removeConn(before)
	select {
	case <-manager.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "the manager is not done once its connections are gone")
	}
}

func TestConnManager_drain_Empty(t *testing.T) {
	manager := newConnManager()

	assert.Empty(t, manager.drain())
	assert.False(t, manager.tracks(&FakeStreamConn{}))
}
