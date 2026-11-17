package msf

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogRefTrack returns a packaging "catalog" track referencing name in
// namespace (inherited when empty).
func catalogRefTrack(namespace, name string) Track {
	return Track{Namespace: namespace, Name: name, Packaging: PackagingCatalog, IsLive: new(true)}
}

// TestCatalog_CatalogRefsSpecExample uses the catalog of draft-ietf-moq-msf-02
// §5.6.16.
func TestCatalog_CatalogRefsSpecExample(t *testing.T) {
	catalog, err := ParseCatalogString(`{
		"version": "draft-02",
		"generatedAt": 1746104606044,
		"tracks": [
			{"name": "premium-catalog", "isLive": true, "packaging": "catalog"},
			{"name": "free-catalog", "isLive": true, "packaging": "catalog"},
			{"name": "camera33-catalog", "namespace": "example.com/blimps-R-us", "packaging": "catalog",
			 "isLive": true, "label": "Eye From the Sky video feeds"}
		]
	}`)
	require.NoError(t, err)
	catalog.DefaultNamespace = "example.com/live"
	require.NoError(t, catalog.Validate())

	assert.Equal(t, []TrackID{
		{Namespace: "example.com/live", Name: "premium-catalog"},
		{Namespace: "example.com/live", Name: "free-catalog"},
		{Namespace: "example.com/blimps-R-us", Name: "camera33-catalog"},
	}, catalog.CatalogRefs())
}

func TestWalkCatalogRefs(t *testing.T) {
	root := TrackID{Namespace: "example.com/live", Name: "catalog"}
	a := TrackID{Namespace: "example.com/live", Name: "a"}
	b := TrackID{Namespace: "example.com/live", Name: "b"}
	other := TrackID{Namespace: "other.com/feeds", Name: "c"}

	// wide gives root fanOut references to catalogs that each reference the
	// next level, to model exponential fan-out within the depth limit.
	wide := func(fanOut int) map[TrackID][]Track {
		catalogs := map[TrackID][]Track{}
		var refs []Track
		for i := range fanOut {
			name := fmt.Sprintf("n%d", i)
			refs = append(refs, catalogRefTrack("", name))
			var children []Track
			for j := range fanOut {
				children = append(children, catalogRefTrack("", fmt.Sprintf("n%d-%d", i, j)))
				catalogs[TrackID{Namespace: root.Namespace, Name: fmt.Sprintf("n%d-%d", i, j)}] = nil
			}
			catalogs[TrackID{Namespace: root.Namespace, Name: name}] = children
		}
		catalogs[root] = refs
		return catalogs
	}

	tests := map[string]struct {
		catalogs map[TrackID][]Track
		limits   CatalogWalkLimits
		visited  []TrackID
		visits   int
		wantErr  error
	}{
		"wide fan-out stops at the catalog limit": {
			catalogs: wide(10),
			limits:   CatalogWalkLimits{MaxCatalogs: 20},
			visits:   20,
			wantErr:  ErrTooManyCatalogs,
		},
		"wide fan-out within limits": {
			catalogs: wide(3),
			visits:   13,
		},
		"too many references in one catalog": {
			catalogs: wide(5),
			limits:   CatalogWalkLimits{MaxRefsPerCatalog: 4},
			visits:   0,
			wantErr:  ErrTooManyCatalogRefs,
		},
		"tree with inherited and declared namespaces": {
			catalogs: map[TrackID][]Track{
				root:  {catalogRefTrack("", "a"), catalogRefTrack("other.com/feeds", "c")},
				a:     {},
				other: {},
			},
			visited: []TrackID{root, a, other},
		},
		"diamond is visited once": {
			catalogs: map[TrackID][]Track{
				root: {catalogRefTrack("", "a"), catalogRefTrack("", "b")},
				a:    {catalogRefTrack("", "b")},
				b:    {},
			},
			visited: []TrackID{root, a, b},
		},
		"self reference": {
			catalogs: map[TrackID][]Track{root: {catalogRefTrack("", "catalog")}},
			visited:  []TrackID{root},
			wantErr:  ErrCatalogCycle,
		},
		"two catalog cycle": {
			catalogs: map[TrackID][]Track{
				root: {catalogRefTrack("", "a")},
				a:    {catalogRefTrack("", "catalog")},
			},
			visited: []TrackID{root, a},
			wantErr: ErrCatalogCycle,
		},
		"depth exceeded": {
			catalogs: map[TrackID][]Track{
				root: {catalogRefTrack("", "a")},
				a:    {catalogRefTrack("", "b")},
				b:    {},
			},
			limits:  CatalogWalkLimits{MaxDepth: 1},
			visited: []TrackID{root, a},
			wantErr: ErrCatalogDepth,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			loads := map[TrackID]int{}
			load := func(_ context.Context, id TrackID) (Catalog, error) {
				loads[id]++
				tracks, ok := tt.catalogs[id]
				if !ok {
					return Catalog{}, fmt.Errorf("no catalog %s", id)
				}
				return Catalog{Version: CurrentVersion, Tracks: tracks}, nil
			}
			var visited []TrackID
			visit := func(id TrackID, catalog Catalog) error {
				visited = append(visited, id)
				assert.Equal(t, id.Namespace, catalog.DefaultNamespace, "a loaded catalog inherits the namespace it was loaded from")
				return nil
			}

			err := WalkCatalogRefs(ctx, root, tt.limits, load, visit)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			if tt.visited != nil {
				assert.Equal(t, tt.visited, visited)
			} else {
				assert.Len(t, visited, tt.visits)
			}
			for id, n := range loads {
				assert.Equal(t, 1, n, "catalog %s loaded more than once", id)
			}
		})
	}
}

func TestWalkCatalogRefs_PropagatesErrors(t *testing.T) {
	root := TrackID{Namespace: "example.com/live", Name: "catalog"}
	errLoad := errors.New("load failed")
	errVisit := errors.New("visit failed")
	ok := func(context.Context, TrackID) (Catalog, error) { return Catalog{Version: CurrentVersion}, nil }

	tests := map[string]struct {
		load    func(context.Context, TrackID) (Catalog, error)
		visit   func(TrackID, Catalog) error
		wantErr error
	}{
		"load error": {
			load:    func(context.Context, TrackID) (Catalog, error) { return Catalog{}, errLoad },
			visit:   func(TrackID, Catalog) error { return nil },
			wantErr: errLoad,
		},
		"visit error": {
			load:    ok,
			visit:   func(TrackID, Catalog) error { return errVisit },
			wantErr: errVisit,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			assert.ErrorIs(t, WalkCatalogRefs(ctx, root, CatalogWalkLimits{}, tt.load, tt.visit), tt.wantErr)
		})
	}
}

func TestWalkCatalogRefs_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	load := func(context.Context, TrackID) (Catalog, error) { return Catalog{}, nil }
	visit := func(TrackID, Catalog) error { return nil }

	err := WalkCatalogRefs(ctx, TrackID{Namespace: "live", Name: "catalog"}, CatalogWalkLimits{}, load, visit)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestWalkCatalogRefs_RejectsUnusableRoot checks that a root without a usable
// namespace or name is refused before load runs, so load never sees the
// internal inherited-namespace marker.
func TestWalkCatalogRefs_RejectsUnusableRoot(t *testing.T) {
	tests := map[string]struct {
		root     TrackID
		errMatch string
	}{
		"no namespace":          {root: TrackID{Name: "catalog"}, errMatch: "needs the Track Namespace"},
		"inherited marker":      {root: Track{Name: "catalog"}.ID(""), errMatch: "needs the Track Namespace"},
		"malformed namespace":   {root: TrackID{Namespace: "a//b", Name: "catalog"}, errMatch: "needs the Track Namespace"},
		"no name":               {root: TrackID{Namespace: "live"}, errMatch: "valid Track Name"},
		"NUL in name":           {root: TrackID{Namespace: "live", Name: "cat\x00alog"}, errMatch: "valid Track Name"},
		"usable root is walked": {root: TrackID{Namespace: "live", Name: "catalog"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var loaded []TrackID
			load := func(_ context.Context, id TrackID) (Catalog, error) {
				loaded = append(loaded, id)
				return Catalog{Version: CurrentVersion, Tracks: []Track{catalogRefTrack("", "child")}}, nil
			}
			visit := func(TrackID, Catalog) error { return nil }

			err := WalkCatalogRefs(ctx, tt.root, CatalogWalkLimits{MaxDepth: 1}, load, visit)
			if tt.errMatch != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
				assert.Empty(t, loaded)
				return
			}
			assert.ErrorIs(t, err, ErrCatalogCycle, "the child references itself")
			for _, id := range loaded {
				assert.Equal(t, "live", id.Namespace, "load only sees real namespaces")
			}
		})
	}
}
