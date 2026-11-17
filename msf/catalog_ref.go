package msf

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DefaultMaxCatalogDepth is the catalog reference depth WalkCatalogRefs
// follows when given a non-positive maxDepth.
const DefaultMaxCatalogDepth = 8

var (
	// ErrCatalogCycle reports a catalog that references itself, directly or
	// through other catalogs (draft-ietf-moq-msf-02 §13.7).
	ErrCatalogCycle = errors.New("msf: catalog reference cycle")
	// ErrCatalogDepth reports a chain of catalog references deeper than the
	// walk allows.
	ErrCatalogDepth = errors.New("msf: catalog reference depth exceeded")
)

// CatalogRefs returns the Full Track Names of the tracks in c that reference
// other catalogs (packaging "catalog", draft-ietf-moq-msf-02 §5), in catalog
// order. A track without a namespace inherits c.DefaultNamespace.
func (c Catalog) CatalogRefs() []TrackID {
	var refs []TrackID
	for i := range c.Tracks {
		if c.Tracks[i].Packaging == PackagingCatalog {
			refs = append(refs, c.Tracks[i].ID(c.DefaultNamespace))
		}
	}
	return refs
}

// WalkCatalogRefs visits the catalog root and, depth first, every catalog it
// references through packaging "catalog" tracks. load fetches the catalog
// published on a track; the walk sets the loaded catalog's DefaultNamespace to
// the namespace of the track it was loaded from when the catalog does not set
// one, so its tracks inherit that namespace. visit is called once per distinct
// catalog, before its references are followed.
//
// A reference back to a catalog on the current path is a cycle and stops the
// walk with ErrCatalogCycle, as subscribers MUST detect cycles
// (draft-ietf-moq-msf-02 §13.7). A catalog reached again through a different
// path is not a cycle and is not visited twice. Chains longer than maxDepth
// references (DefaultMaxCatalogDepth when maxDepth <= 0) stop the walk with
// ErrCatalogDepth. Errors from load and visit are returned as they are, after
// wrapping.
func WalkCatalogRefs(ctx context.Context, root TrackID, maxDepth int, load func(context.Context, TrackID) (Catalog, error), visit func(TrackID, Catalog) error) error {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxCatalogDepth
	}
	w := catalogWalk{
		load:     load,
		visit:    visit,
		maxDepth: maxDepth,
		visited:  make(map[TrackID]struct{}),
		onPath:   make(map[TrackID]struct{}),
	}
	return w.walk(ctx, root, 0)
}

// catalogWalk holds the state of one WalkCatalogRefs call.
type catalogWalk struct {
	load     func(context.Context, TrackID) (Catalog, error)
	visit    func(TrackID, Catalog) error
	maxDepth int
	visited  map[TrackID]struct{}
	onPath   map[TrackID]struct{}
	path     []TrackID
}

// walk visits id and the catalogs it references.
func (w *catalogWalk) walk(ctx context.Context, id TrackID, depth int) error {
	if _, ok := w.onPath[id]; ok {
		return fmt.Errorf("%w: %s", ErrCatalogCycle, w.describePath(id))
	}
	if _, ok := w.visited[id]; ok {
		return nil
	}
	if depth > w.maxDepth {
		return fmt.Errorf("%w: %s", ErrCatalogDepth, w.describePath(id))
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	catalog, err := w.load(ctx, id)
	if err != nil {
		return fmt.Errorf("msf: load catalog %q: %w", id.String(), err)
	}
	if catalog.DefaultNamespace == "" && id.Namespace != inheritedNamespaceSentinel {
		catalog.DefaultNamespace = id.Namespace
	}
	w.visited[id] = struct{}{}
	if err := w.visit(id, catalog); err != nil {
		return fmt.Errorf("msf: visit catalog %q: %w", id.String(), err)
	}

	w.onPath[id] = struct{}{}
	w.path = append(w.path, id)
	defer func() {
		delete(w.onPath, id)
		w.path = w.path[:len(w.path)-1]
	}()

	for _, ref := range catalog.CatalogRefs() {
		if err := w.walk(ctx, ref, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// describePath renders the current reference path followed by next.
func (w *catalogWalk) describePath(next TrackID) string {
	parts := make([]string, 0, len(w.path)+1)
	for _, id := range w.path {
		parts = append(parts, id.String())
	}
	parts = append(parts, next.String())
	return strings.Join(parts, " -> ")
}
