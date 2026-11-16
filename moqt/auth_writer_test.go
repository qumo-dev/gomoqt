package moqt

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthWriter_Grant_InvalidPattern(t *testing.T) {
	scope := newAuthScope(func() {})
	w := &AuthWriter{scope: scope}

	err := w.Grant(Grant{Root: "/", Subscribe: []string{"/room/1/", "room"}})

	assert.ErrorIs(t, err, ErrInvalidPathPattern)
	assert.False(t, scope.canSubscribe("/room/1"), "the invalid grant is not applied")
}

func TestAuthWriter_Grant_ReusedSlice(t *testing.T) {
	changes := 0
	scope := newAuthScope(func() { changes++ })
	w := &AuthWriter{scope: scope}
	g := Grant{Root: "/", Subscribe: []string{"/room/1/"}}
	require.NoError(t, w.Grant(g))

	g.Subscribe[0] = "/room/2/"
	require.NoError(t, w.Grant(g))

	assert.False(t, scope.canSubscribe("/room/1/host"))
	assert.True(t, scope.canSubscribe("/room/2/host"))
	assert.Equal(t, 2, changes, "editing the slice in place and granting again changes the scope")
}

func TestParseGrant(t *testing.T) {
	tests := map[string]struct {
		grant   Grant
		wantErr bool
	}{
		"empty grants nothing":        {grant: Grant{}},
		"root only":                   {grant: Grant{Root: "/room/1/"}},
		"patterns under a root":       {grant: Grant{Root: "/room/1/", Subscribe: []string{"/"}, Publish: []string{"/{user}/audio"}}},
		"patterns without a root":     {grant: Grant{Subscribe: []string{"/"}}, wantErr: true},
		"root that is a path":         {grant: Grant{Root: "/room/1"}, wantErr: true},
		"root with a wildcard":        {grant: Grant{Root: "/room/{id}/"}, wantErr: true},
		"pattern without a slash":     {grant: Grant{Root: "/room/1/", Subscribe: []string{"host"}}, wantErr: true},
		"pattern with a bad wildcard": {grant: Grant{Root: "/room/1/", Subscribe: []string{"/{a"}}, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseGrant(tt.grant)

			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidPathPattern)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestParseGrant_PatternsJoinRoot(t *testing.T) {
	g, err := parseGrant(Grant{Root: "/room/1/", Subscribe: []string{"/{user}/audio"}})
	require.NoError(t, err)

	assert.True(t, matchAny(g.subscribe, "/room/1/alice/audio"))
	assert.False(t, matchAny(g.subscribe, "/room/2/alice/audio"))
	assert.False(t, matchAny(g.subscribe, "/alice/audio"))
}

func TestAuthScope_canList(t *testing.T) {
	scope := newAuthScope(func() {})
	w := &AuthWriter{scope: scope}
	require.NoError(t, w.Grant(Grant{Root: "/room/1/"}))

	tests := map[string]struct {
		prefix prefix
		want   bool
	}{
		"the root":         {prefix: "/room/1/", want: true},
		"beneath the root": {prefix: "/room/1/alice/", want: true},
		"above the root":   {prefix: "/room/", want: false},
		"another root":     {prefix: "/room/2/", want: false},
		"a sibling path":   {prefix: "/room/10/", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, scope.canList(tt.prefix))
		})
	}
}

func TestAuthWriter_Grant_NegativeMaxAge(t *testing.T) {
	scope := newAuthScope(func() {})
	w := &AuthWriter{scope: scope}

	err := w.Grant(Grant{Root: "/", Subscribe: []string{"/"}, MaxAge: -time.Second})

	assert.Error(t, err)
	assert.False(t, scope.canSubscribe("/room/1"), "the invalid grant is not applied")
}

func TestAuthWriter_Grant_MaxAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scope := newAuthScope(func() {})
		w := &AuthWriter{scope: scope}
		require.NoError(t, w.Grant(Grant{Root: "/", Subscribe: []string{"/"}, MaxAge: time.Second}))

		time.Sleep(2 * time.Second)
		synctest.Wait()

		assert.False(t, scope.canSubscribe("/room/1"))
		assert.ErrorIs(t, w.Grant(Grant{}), ErrAuthEnded, "an expired grant ends its token")
	})
}

func TestAuthWriter_Grant_RegrantExtends(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scope := newAuthScope(func() {})
		w := &AuthWriter{scope: scope}
		g := Grant{Root: "/", Subscribe: []string{"/"}, MaxAge: time.Second}
		require.NoError(t, w.Grant(g))

		time.Sleep(700 * time.Millisecond)
		require.NoError(t, w.Grant(g))
		time.Sleep(700 * time.Millisecond)
		synctest.Wait()

		assert.True(t, scope.canSubscribe("/room/1"), "the second grant restarts the expiry")
	})
}

func TestAuthScope_set_SamePatterns(t *testing.T) {
	changes := 0
	scope := newAuthScope(func() { changes++ })
	w := &AuthWriter{scope: scope}
	g, err := parseGrant(Grant{Root: "/", Subscribe: []string{"/room/1/"}})
	require.NoError(t, err)
	again, err := parseGrant(Grant{Root: "/", Subscribe: []string{"/room/1/"}, MaxAge: time.Minute})
	require.NoError(t, err)

	scope.set(w, g)
	scope.set(w, again)

	assert.Equal(t, 1, changes, "extending a grant does not change the scope")
}

func TestAuthReason(t *testing.T) {
	long := strings.Repeat("あ", maxAuthReasonLength)
	tests := map[string]struct {
		reason string
		want   string
	}{
		"valid":        {reason: "unknown ticket", want: "unknown ticket"},
		"invalid utf8": {reason: "bad\xffbyte", want: "bad�byte"},
		"too long":     {reason: long, want: long[:maxAuthReasonLength-maxAuthReasonLength%3]},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := authReason(tt.reason)

			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got))
			assert.LessOrEqual(t, len(got), maxAuthReasonLength)
		})
	}
}

// fakeScopeWatcher counts its rescopes.
type fakeScopeWatcher struct{ rescopes int }

func (w *fakeScopeWatcher) rescope() { w.rescopes++ }

var _ scopeWatcher = (*fakeScopeWatcher)(nil)

func TestAuthScope_watch(t *testing.T) {
	scope := newAuthScope(func() {})
	w := &fakeScopeWatcher{}

	unwatch := scope.watch(w)
	require.Equal(t, []scopeWatcher{w}, scope.watching())
	unwatch()

	assert.Empty(t, scope.watching())
}

func TestAnnouncementWriter_rescope(t *testing.T) {
	tests := map[string]struct {
		prefix      prefix
		closedFirst bool
		wantReset   bool
	}{
		"within the root":           {prefix: "/room/1/"},
		"outside every root":        {prefix: "/room/2/", wantReset: true},
		"outside, but closed first": {prefix: "/room/2/", closedFirst: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scope := newAuthScope(func() {})
			require.NoError(t, (&AuthWriter{scope: scope}).Grant(Grant{Root: "/room/1/"}))
			stream := &fakeQUICStream{}
			aw := newAnnouncementWriter(stream, tt.prefix, 0, 0, nil, scope)
			if tt.closedFirst {
				require.NoError(t, aw.Close())
			}

			aw.rescope()

			assert.Equal(t, tt.wantReset, slices.Contains(stream.CancelWriteCodes(), transport.StreamErrorCode(AnnounceErrorCodeUnauthorized)))
		})
	}
}

func TestNewAnnouncementWriter_UnwatchesWhenTheStreamEnds(t *testing.T) {
	scope := newAuthScope(func() {})
	aw := newAnnouncementWriter(&fakeQUICStream{}, "/room/1/", 0, 0, nil, scope)
	require.Len(t, scope.watching(), 1)

	require.NoError(t, aw.Close())

	assert.Eventually(t, func() bool { return len(scope.watching()) == 0 }, time.Second, time.Millisecond)
}
