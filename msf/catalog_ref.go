package msf

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Default limits WalkCatalogRefs applies to a CatalogWalkLimits field left at
// zero or below.
const (
	// DefaultMaxCatalogDepth is the default longest chain of catalog
	// references followed from the root.
	DefaultMaxCatalogDepth = 8
	// DefaultMaxCatalogs is the default number of distinct catalogs loaded
	// in one walk, the root included.
	DefaultMaxCatalogs = 64
	// DefaultMaxCatalogRefs is the default number of catalog references a
	// single catalog may hold.
	DefaultMaxCatalogRefs = 32
)

var (
	// ErrCatalogCycle reports a catalog that references itself, directly or
	// through other catalogs (draft-ietf-moq-msf-02 §13.7).
	ErrCatalogCycle = errors.New("msf: catalog reference cycle")
	// ErrCatalogDepth reports a chain of catalog references deeper than
	// CatalogWalkLimits.MaxDepth.
	ErrCatalogDepth = errors.New("msf: catalog reference depth exceeded")
	// ErrTooManyCatalogs reports a walk that would load more distinct
	// catalogs than CatalogWalkLimits.MaxCatalogs.
	ErrTooManyCatalogs = errors.New("msf: too many referenced catalogs")
	// ErrTooManyCatalogRefs reports a catalog holding more catalog references
	// than CatalogWalkLimits.MaxRefsPerCatalog.
	ErrTooManyCatalogRefs = errors.New("msf: too many catalog references in one catalog")
)

// CatalogWalkLimits bounds the resources WalkCatalogRefs spends, since
// subscribers SHOULD bound the catalog references they follow
// (draft-ietf-moq-msf-02 §13.7). A field at zero or below takes its default.
type CatalogWalkLimits struct {
	// MaxDepth is the longest chain of references followed from the root
	// (DefaultMaxCatalogDepth).
	MaxDepth int
	// MaxCatalogs is the number of distinct catalogs loaded, the root
	// included (DefaultMaxCatalogs). It bounds wide fan-out, which a depth
	// limit alone lets grow exponentially.
	MaxCatalogs int
	// MaxRefsPerCatalog is the number of catalog references one catalog may
	// hold (DefaultMaxCatalogRefs).
	MaxRefsPerCatalog int
}

// withDefaults returns l with every unset field at its default.
func (l CatalogWalkLimits) withDefaults() CatalogWalkLimits {
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultMaxCatalogDepth
	}
	if l.MaxCatalogs <= 0 {
		l.MaxCatalogs = DefaultMaxCatalogs
	}
	if l.MaxRefsPerCatalog <= 0 {
		l.MaxRefsPerCatalog = DefaultMaxCatalogRefs
	}
	return l
}

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
// Each distinct catalog is loaded at most once: a catalog reached again
// through a different path (a diamond) is skipped. A reference back to a
// catalog on the current path is a cycle and stops the walk with
// ErrCatalogCycle, as subscribers MUST detect cycles (draft-ietf-moq-msf-02
// §13.7). Exceeding limits stops the walk with ErrCatalogDepth,
// ErrTooManyCatalogs or ErrTooManyCatalogRefs. Errors from load and visit are
// returned wrapped.
func WalkCatalogRefs(ctx context.Context, root TrackID, limits CatalogWalkLimits, load func(context.Context, TrackID) (Catalog, error), visit func(TrackID, Catalog) error) error {
	w := catalogWalk{
		load:    load,
		visit:   visit,
		limits:  limits.withDefaults(),
		visited: make(map[TrackID]struct{}),
		onPath:  make(map[TrackID]struct{}),
	}
	return w.walk(ctx, root, 0)
}

// catalogWalk holds the state of one WalkCatalogRefs call.
type catalogWalk struct {
	load    func(context.Context, TrackID) (Catalog, error)
	visit   func(TrackID, Catalog) error
	limits  CatalogWalkLimits
	visited map[TrackID]struct{}
	onPath  map[TrackID]struct{}
	path    []TrackID
}

// walk visits id and the catalogs it references.
func (w *catalogWalk) walk(ctx context.Context, id TrackID, depth int) error {
	if _, ok := w.onPath[id]; ok {
		return fmt.Errorf("%w: %s", ErrCatalogCycle, w.describePath(id))
	}
	if _, ok := w.visited[id]; ok {
		return nil
	}
	if depth > w.limits.MaxDepth {
		return fmt.Errorf("%w: %s", ErrCatalogDepth, w.describePath(id))
	}
	if len(w.visited) >= w.limits.MaxCatalogs {
		return fmt.Errorf("%w: limit %d reached at %s", ErrTooManyCatalogs, w.limits.MaxCatalogs, w.describePath(id))
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
	refs := catalog.CatalogRefs()
	if len(refs) > w.limits.MaxRefsPerCatalog {
		return fmt.Errorf("%w: catalog %q holds %d, limit %d", ErrTooManyCatalogRefs, id.String(), len(refs), w.limits.MaxRefsPerCatalog)
	}
	if err := w.visit(id, catalog); err != nil {
		return fmt.Errorf("msf: visit catalog %q: %w", id.String(), err)
	}

	w.onPath[id] = struct{}{}
	w.path = append(w.path, id)
	defer func() {
		delete(w.onPath, id)
		w.path = w.path[:len(w.path)-1]
	}()

	for _, ref := range refs {
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
