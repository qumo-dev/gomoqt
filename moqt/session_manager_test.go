package moqt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager_AddSession(t *testing.T) {
	tests := map[string]struct {
		sessions  []*Session
		wantCount int
	}{
		"nil session is ignored": {
			sessions:  []*Session{nil},
			wantCount: 0,
		},
		"duplicate session is counted once": {
			sessions: func() []*Session {
				session := &Session{}
				return []*Session{session, session}
			}(),
			wantCount: 1,
		},
		"multiple distinct sessions are counted": {
			sessions:  []*Session{&Session{}, &Session{}},
			wantCount: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := newSessionManager()
			for _, session := range tt.sessions {
				manager.addSession(session)
			}

			assert.Equal(t, tt.wantCount, manager.countSessions())
		})
	}
}

func TestSessionManager_RemoveSession(t *testing.T) {
	manager := newSessionManager()
	first := &Session{}
	second := &Session{}
	manager.addSession(first)
	manager.addSession(second)
	done := manager.Done()

	manager.removeSession(nil)
	assert.Equal(t, 2, manager.countSessions())

	manager.removeSession(first)
	assert.Equal(t, 1, manager.countSessions())

	select {
	case <-done:
		t.Fatal("done channel should not be closed while sessions remain")
	default:
	}

	manager.removeSession(second)
	assert.Equal(t, 0, manager.countSessions())

	select {
	case <-done:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel did not close after removing last session")
	}

	// Removing again should be a no-op.
	manager.removeSession(second)
	assert.Equal(t, 0, manager.countSessions())
}

func TestSessionManager_Done(t *testing.T) {
	manager := newSessionManager()
	first := manager.Done()

	select {
	case <-first:
		// expected: no sessions means we're already done
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel should be closed when there are no tracked sessions")
	}

	session := &Session{}
	manager.addSession(session)
	second := manager.Done()
	assert.NotEqual(t, first, second)

	select {
	case <-second:
		t.Fatal("done channel should not be closed while a session is active")
	default:
	}

	manager.removeSession(session)

	select {
	case <-second:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel should close once the tracked session count reaches zero")
	}
}

func TestSessionManager_Close(t *testing.T) {
	tests := map[string]struct {
		setup   func() *sessionManager
		wantErr string
	}{
		"empty manager closes": {
			setup: func() *sessionManager {
				return newSessionManager()
			},
		},
		"active sessions prevent close": {
			setup: func() *sessionManager {
				manager := newSessionManager()
				manager.addSession(&Session{})
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
		manager := newSessionManager()
		require.NoError(t, manager.Close())
		require.NoError(t, manager.Close())
	})

	t.Run("closed manager ignores new sessions", func(t *testing.T) {
		manager := newSessionManager()
		require.NoError(t, manager.Close())

		manager.addSession(&Session{})
		assert.Zero(t, manager.countSessions())
	})
}
