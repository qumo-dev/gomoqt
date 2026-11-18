package moqt

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patternHandlersOf snapshots mux's pattern handlers under its lock: a
// replaced or ended handler is removed from another goroutine.
func patternHandlersOf(tb testing.TB, mux *TrackMux) []*patternHandler {
	tb.Helper()
	mux.mu.RLock()
	defer mux.mu.RUnlock()
	return slices.Clone(mux.patternHandlers)
}

// mustPathPattern parses s, failing tb if it is invalid.
func mustPathPattern(tb testing.TB, s string) PathPattern {
	tb.Helper()
	p, err := ParsePathPattern(s)
	require.NoError(tb, err)
	return p
}

// servedBy registers a handler for each pattern on mux and reports which
// pattern serves a subscription to path, "" for none.
func servedBy(tb testing.TB, mux *TrackMux, patterns []string, path BroadcastPath) string {
	tb.Helper()
	served := ""
	for _, p := range patterns {
		mux.HandleFunc(tb.Context(), p, func(*TrackWriter) { served = p })
	}
	mux.serveTrack(&TrackWriter{BroadcastPath: path})
	return served
}

func TestTrackMux_Handle(t *testing.T) {
	mux := NewTrackMux(0)
	var id, rest string
	mux.HandleFunc(t.Context(), "/room/{id}/{rest...}", func(tw *TrackWriter) {
		id, rest = tw.PathValue("id"), tw.PathValue("rest")
	})

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/1/alice/cam"})

	assert.Equal(t, "1", id)
	assert.Equal(t, "alice/cam", rest)
}

func TestTrackMux_Handle_Precedence(t *testing.T) {
	tests := map[string]struct {
		patterns []string
		path     BroadcastPath
		want     string
	}{
		"literal beats wildcard":           {patterns: []string{"/room/{id}/host", "/room/1/host"}, path: "/room/1/host", want: "/room/1/host"},
		"wildcard beats prefix":            {patterns: []string{"/room/", "/room/{id}"}, path: "/room/1", want: "/room/{id}"},
		"longer literal beats prefix":      {patterns: []string{"/room/{id}/", "/room/{id}/host"}, path: "/room/1/host", want: "/room/{id}/host"},
		"root serves what nothing else":    {patterns: []string{"/", "/room/{id}"}, path: "/lobby", want: "/"},
		"anchor beats prefix":              {patterns: []string{"/room/", "/room/{$}"}, path: "/room/", want: "/room/{$}"},
		"no matching pattern serves none":  {patterns: []string{"/room/{id}"}, path: "/lobby/1", want: ""},
		"exact path beats deeper wildcard": {patterns: []string{"/room/{id}/{rest...}", "/room/1"}, path: "/room/1", want: "/room/1"},
		"order of registration aside":      {patterns: []string{"/room/1/host", "/room/{id}/host"}, path: "/room/1/host", want: "/room/1/host"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := servedBy(t, NewTrackMux(0), tt.patterns, tt.path)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrackMux_Handle_Conflict(t *testing.T) {
	tests := map[string]struct {
		first, second string
		wantPanic     bool
	}{
		"overlapping, neither more specific": {first: "/{a}/host", second: "/room/{b}", wantPanic: true},
		"same paths, other names":            {first: "/room/{id}", second: "/room/{name}", wantPanic: true},
		"prefix and rest wildcard":           {first: "/room/", second: "/room/{rest...}", wantPanic: true},
		"one more specific":                  {first: "/room/{id}", second: "/room/1"},
		"disjoint literals":                  {first: "/room/{id}", second: "/lobby/{id}"},
		"empty segment and wildcard":         {first: "/room//x", second: "/room/{id}/x"},
		"exact and its prefix":               {first: "/room", second: "/room/"},
		"same pattern again":                 {first: "/room/{id}", second: "/room/{id}"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mux := NewTrackMux(0)
			mux.HandleFunc(t.Context(), tt.first, func(*TrackWriter) {})

			register := func() { mux.HandleFunc(t.Context(), tt.second, func(*TrackWriter) {}) }

			if tt.wantPanic {
				assert.Panics(t, register)
				return
			}
			assert.NotPanics(t, register)
		})
	}
}

func TestPathPattern_compare(t *testing.T) {
	tests := map[string]struct {
		a, b string
		want patternRelation
	}{
		"same literal":               {a: "/room/1", b: "/room/1", want: relEquivalent},
		"literal under wildcard":     {a: "/room/1", b: "/room/{id}", want: relMoreSpecific},
		"wildcard over literal":      {a: "/room/{id}", b: "/room/1", want: relMoreGeneral},
		"path under prefix":          {a: "/room/1/a", b: "/room/", want: relMoreSpecific},
		"root over all":              {a: "/", b: "/room/{id}/", want: relMoreGeneral},
		"crossing wildcards":         {a: "/{a}/host", b: "/room/{b}", want: relOverlaps},
		"different literals":         {a: "/room/1", b: "/room/2", want: relDisjoint},
		"different lengths":          {a: "/room/{id}", b: "/room/{id}/host", want: relDisjoint},
		"anchor under prefix":        {a: "/room/{$}", b: "/room/", want: relMoreSpecific},
		"anchor apart from wildcard": {a: "/room/{$}", b: "/room/{id}", want: relDisjoint},
		"prefix equals rest":         {a: "/room/", b: "/room/{rest...}", want: relEquivalent},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := mustPathPattern(t, tt.a).compare(mustPathPattern(t, tt.b))

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrackMux_Handle_AnnouncedPathWins(t *testing.T) {
	mux := NewTrackMux(0)
	served := ""
	mux.HandleFunc(t.Context(), "/room/{id}/", func(*TrackWriter) { served = "pattern" })
	mux.PublishFunc(t.Context(), "/room/1/alice", func(*TrackWriter) { served = "announced" })

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/1/alice"})

	assert.Equal(t, "announced", served)
}

func TestTrackMux_Handle_AnnouncedRouteAboveWins(t *testing.T) {
	mux := NewTrackMux(0)
	served := ""
	mux.HandleFunc(t.Context(), "/room/{id}/", func(*TrackWriter) { served = "pattern" })
	mux.PublishFunc(t.Context(), "/room/1", func(*TrackWriter) { served = "route" })

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/1/alice"})

	assert.Equal(t, "route", served)
}

func TestTrackMux_Handle_ServesBesideAnotherRoute(t *testing.T) {
	mux := NewTrackMux(0)
	var id string
	mux.HandleFunc(t.Context(), "/room/{id}/", func(tw *TrackWriter) { id = tw.PathValue("id") })
	mux.PublishFunc(t.Context(), "/room/1", func(*TrackWriter) {})

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/2/alice"})

	assert.Equal(t, "2", id)
}

func TestTrackMux_Handle_ReplacesSamePattern(t *testing.T) {
	mux := NewTrackMux(0)
	served := ""
	mux.HandleFunc(t.Context(), "/room/{id}", func(*TrackWriter) { served = "first" })
	mux.HandleFunc(t.Context(), "/room/{id}", func(*TrackWriter) { served = "second" })

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/1"})

	assert.Equal(t, "second", served)
	assert.Len(t, patternHandlersOf(t, mux), 1)
}

func TestTrackMux_Handle_EndsWithContext(t *testing.T) {
	mux := NewTrackMux(0)
	ctx, cancel := context.WithCancel(t.Context())
	mux.HandleFunc(ctx, "/room/{id}", func(*TrackWriter) {})

	cancel()

	assert.Eventually(t, func() bool {
		_, _, ok := mux.handler("/room/1")
		return !ok
	}, time.Second, time.Millisecond)
}

func TestTrackMux_Handle_InvalidPattern(t *testing.T) {
	tests := map[string]string{
		"no leading /":        "room/{id}",
		"wildcard in segment": "/room/a{id}",
	}

	for name, pattern := range tests {
		t.Run(name, func(t *testing.T) {
			mux := NewTrackMux(0)

			assert.Panics(t, func() { mux.HandleFunc(t.Context(), pattern, func(*TrackWriter) {}) })
		})
	}
}

func TestTrackMux_TrackHandler_Pattern(t *testing.T) {
	mux := NewTrackMux(0)
	served := false
	mux.HandleFunc(t.Context(), "/room/{id}", func(*TrackWriter) { served = true })

	ann, handler := mux.TrackHandler("/room/1")
	require.NotNil(t, handler)
	handler.ServeTrack(&TrackWriter{BroadcastPath: "/room/1"})

	assert.Nil(t, ann)
	assert.True(t, served)
}

func TestTrackMux_ServeTrack_RoutesARewrittenPathAgain(t *testing.T) {
	inner := NewTrackMux(0)
	var id, tenant string
	inner.HandleFunc(t.Context(), "/room/{id}/mix", func(tw *TrackWriter) {
		id, tenant = tw.PathValue("id"), tw.PathValue("tenant")
	})
	outer := NewTrackMux(0)
	var outerTenant string
	outer.HandleFunc(t.Context(), "/t/{tenant}/{rest...}", func(tw *TrackWriter) {
		outerTenant = tw.PathValue("tenant")
		tw.BroadcastPath = BroadcastPath("/" + tw.PathValue("rest"))
		inner.ServeTrack(tw)
	})

	outer.ServeTrack(&TrackWriter{BroadcastPath: "/t/acme/room/42/mix"})

	assert.Equal(t, "acme", outerTenant)
	assert.Equal(t, "42", id)
	assert.Equal(t, "", tenant, "routing again replaces the outer values")
}

func TestTrackMux_ServeTrack_AnnouncedPathClearsValues(t *testing.T) {
	inner := NewTrackMux(0)
	value := "unset"
	inner.PublishFunc(t.Context(), "/room/42", func(tw *TrackWriter) { value = tw.PathValue("id") })
	outer := NewTrackMux(0)
	outer.HandleFunc(t.Context(), "/{id}", func(tw *TrackWriter) {
		tw.BroadcastPath = BroadcastPath("/room/" + tw.PathValue("id"))
		inner.ServeTrack(tw)
	})

	outer.ServeTrack(&TrackWriter{BroadcastPath: "/42"})

	assert.Equal(t, "", value)
}

func TestTrackWriter_PathValue_Announced(t *testing.T) {
	mux := NewTrackMux(0)
	value := "unset"
	mux.PublishFunc(t.Context(), "/room/1", func(tw *TrackWriter) { value = tw.PathValue("id") })

	mux.serveTrack(&TrackWriter{BroadcastPath: "/room/1"})

	assert.Equal(t, "", value)
}

func TestTrackMux_Handle_RejectsUnusableHandlers(t *testing.T) {
	mux := NewTrackMux(0)
	var nilMux *TrackMux
	tests := map[string]TrackHandler{
		"nil func":    TrackHandlerFunc(nil),
		"nil mux":     nilMux,
		"itself":      mux,
		"nil handler": nil,
	}

	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() { mux.Handle(t.Context(), "/room/{id}", handler) })
		})
	}
}

func TestTrackMux_Handle_EndingRefusesServedTracks(t *testing.T) {
	mux := NewTrackMux(0)
	ctx, cancel := context.WithCancel(t.Context())
	serving := make(chan struct{})
	mux.HandleFunc(ctx, "/room/{id}", func(tw *TrackWriter) {
		close(serving)
		<-tw.Context().Done()
	})
	stream := &fakeQUICStream{}
	tw := newTrackWriter("/room/1", "video", newReceiveSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{}),
		func(context.Context) (transport.SendStream, error) { return &fakeQUICSendStream{}, nil }, func() {})
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.serveTrack(tw)
	}()
	<-serving

	cancel()

	<-done
	var streamErr *transport.StreamError
	require.ErrorAs(t, context.Cause(stream.Context()), &streamErr)
	assert.Equal(t, transport.StreamErrorCode(SubscribeErrorCodeNotFound), streamErr.ErrorCode)
}

func TestTrackMux_Handle_ReplacementEndsTheOldHandler(t *testing.T) {
	mux := NewTrackMux(0)
	mux.HandleFunc(t.Context(), "/room/{id}", func(*TrackWriter) {})
	old := patternHandlersOf(t, mux)[0]

	mux.HandleFunc(t.Context(), "/room/{id}", func(*TrackWriter) {})

	assert.Error(t, old.ctx.Err(), "the tracks the old handler serves are closed")
	assert.NotSame(t, old, patternHandlersOf(t, mux)[0])
}

func TestTrackMux_Handle_EndedContext(t *testing.T) {
	mux := NewTrackMux(0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	mux.HandleFunc(ctx, "/room/{id}", func(*TrackWriter) {})

	assert.Empty(t, patternHandlersOf(t, mux))
}
