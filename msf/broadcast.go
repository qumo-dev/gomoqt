package msf

import (
	"fmt"
	"sync"

	"github.com/okdaichi/gomoqt/moqt"
)

// DefaultCatalogTrackName is the reserved MSF catalog track name from draft-00.
const DefaultCatalogTrackName moqt.TrackName = "catalog"

// Broadcast is an MSF-aware moqt.TrackHandler that serves the catalog track and
// routes other track subscriptions to registered per-track handlers.
//
// It keeps the current catalog snapshot and handler table together so callers
// can manage a broadcast through a single object. Track replacement and
// removal close any active TrackWriter values for that track; handlers are
// expected to return once their writer is closed.
//
// Because the underlying moqt routing key is a single TrackName, Broadcast
// requires track names to be unique across namespaces within the current
// catalog snapshot. Catalogs that contain the same track name in multiple
// namespaces are rejected rather than being mapped to synthetic routing names.
type Broadcast struct {
	mu sync.RWMutex

	catalogTrackName moqt.TrackName
	catalog          Catalog
	tracks           moqt.Broadcast
}

// initLocked initializes lazily allocated broadcast defaults while b.mu is held.
func (b *Broadcast) initLocked() {
	if b.catalogTrackName == "" {
		b.catalogTrackName = DefaultCatalogTrackName
	}
}

// NewBroadcast constructs a Broadcast using an initial independent catalog.
// The catalog is validated before the broadcast is returned; an error is
// returned if validation fails.
//
// Clients may also declare a zero-value Broadcast and call SetCatalog later;
// NewBroadcast is provided as a convenience initializer.
func NewBroadcast(catalog Catalog) (*Broadcast, error) {
	b := &Broadcast{
		catalogTrackName: DefaultCatalogTrackName,
	}
	if err := b.SetCatalog(catalog); err != nil {
		return nil, err
	}
	return b, nil
}

// CatalogTrackName returns the reserved track name used for catalog delivery.
func (b *Broadcast) CatalogTrackName() moqt.TrackName {
	if b == nil || b.catalogTrackName == "" {
		return DefaultCatalogTrackName
	}
	return b.catalogTrackName
}

// Catalog returns a deep copy of the current catalog snapshot.
func (b *Broadcast) Catalog() Catalog {
	if b == nil {
		return Catalog{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.catalog.Clone()
}

// CatalogBytes returns the current catalog snapshot encoded as JSON.  The
// result is a deep copy, so callers may modify the returned bytes without
// affecting the broadcast state.  If the receiver is nil an error is returned.
func (b *Broadcast) CatalogBytes() ([]byte, error) {
	if b == nil {
		return nil, fmt.Errorf("msf: nil broadcast")
	}
	b.mu.RLock()
	catalog := b.catalog.Clone()
	b.mu.RUnlock()
	return catalog.MarshalJSON()
}

// SetCatalog replaces the current independent catalog snapshot. The provided
// catalog is cloned and validated before being stored. Concurrent readers of
// Catalog() will see either the old or new snapshot, but not a partially
// applied state.
func (b *Broadcast) SetCatalog(catalog Catalog) error {
	if b == nil {
		return fmt.Errorf("msf: nil broadcast")
	}

	catalogTrackName := b.CatalogTrackName()
	clone := catalog.Clone()
	if err := clone.Validate(); err != nil {
		return err
	}
	if err := validateCatalogForBroadcast(clone, catalogTrackName); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.initLocked()
	staleTrackNames := b.staleTrackNamesLocked(clone)
	b.catalog = clone
	for _, name := range staleTrackNames {
		b.tracks.Remove(name)
	}

	return nil
}

// RegisterTrack adds a new track definition to the catalog and associates a
// handler with it.  If a track with the same name already exists it is
// replaced.  The catalog is validated after modification; the update is
// rejected if validation fails.  The reserved catalog track name may not be
// used, and handler must be non-nil.
func (b *Broadcast) RegisterTrack(track Track, handler moqt.TrackHandler) error {
	if b == nil {
		return fmt.Errorf("msf: nil broadcast")
	}
	if err := validateTrackHandler(handler); err != nil {
		return err
	}
	if track.Name == "" {
		return fmt.Errorf("msf: track name is required")
	}
	if moqt.TrackName(track.Name) == b.CatalogTrackName() {
		return fmt.Errorf("msf: %q is reserved for the catalog track", track.Name)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.initLocked()
	trackClone := track.Clone()
	updated := b.catalog.Clone()
	trackID := trackClone.ID(updated.DefaultNamespace)

	replaced := false
	for i := range updated.Tracks {
		if updated.Tracks[i].Name == trackClone.Name {
			if updated.Tracks[i].ID(updated.DefaultNamespace) != trackID {
				return fmt.Errorf("msf: broadcast requires unique track names across namespaces; duplicate name %q found for %q and %q", trackClone.Name, updated.Tracks[i].ID(updated.DefaultNamespace).String(), trackID.String())
			}
			updated.Tracks[i] = trackClone
			replaced = true
			break
		}
	}
	if !replaced {
		updated.Tracks = append(updated.Tracks, trackClone)
	}
	if err := updated.Validate(); err != nil {
		return err
	}
	if err := validateCatalogForBroadcast(updated, b.catalogTrackName); err != nil {
		return err
	}

	b.catalog = updated
	return b.tracks.Register(moqt.TrackName(trackClone.Name), handler)
}

// RemoveTrack removes the named track (and its handler) from the broadcast.
// It returns true if the track was present.  Attempts to remove the catalog
// track or an empty name have no effect and return false.
func (b *Broadcast) RemoveTrack(name moqt.TrackName) bool {
	if b == nil || name == "" || name == b.CatalogTrackName() {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.initLocked()
	updated := b.catalog.Clone()
	removed := false
	for i := range updated.Tracks {
		if moqt.TrackName(updated.Tracks[i].Name) == name {
			updated.Tracks = append(updated.Tracks[:i], updated.Tracks[i+1:]...)
			removed = true
			break
		}
	}

	b.catalog = updated
	removedFromTracks := b.tracks.Remove(name)

	return removed || removedFromTracks
}

// Handler returns the TrackHandler responsible for serving the named track.
// The special catalog track name is handled by an internal method that
// serializes the current catalog.  If no handler is registered for the name,
// NotFoundTrackHandler is returned.  A nil Broadcast also yields the not-found
// handler.
func (b *Broadcast) Handler(name moqt.TrackName) moqt.TrackHandler {
	if b == nil {
		return moqt.NotFoundTrackHandler
	}
	if name == b.CatalogTrackName() {
		return moqt.TrackHandlerFunc(b.serveCatalogTrack)
	}
	return b.tracks.Handler(name)
}

// ServeTrack implements the moqt.TrackHandler interface by dispatching the
// incoming TrackWriter to the handler determined by Handler().  This allows a
// Broadcast value to be published directly without additional wrappers.
func (b *Broadcast) ServeTrack(tw *moqt.TrackWriter) {
	if tw == nil {
		return
	}
	b.Handler(tw.TrackName).ServeTrack(tw)
}

// serveCatalogTrack serializes the current catalog snapshot onto the reserved catalog track.
func (b *Broadcast) serveCatalogTrack(tw *moqt.TrackWriter) {
	payload, err := b.CatalogBytes()
	if err != nil {
		tw.CloseWithError(moqt.InternalSubscribeErrorCode)
		return
	}

	group, err := tw.OpenGroup()
	if err != nil {
		tw.CloseWithError(moqt.InternalSubscribeErrorCode)
		return
	}

	frame := moqt.NewFrame(len(payload))
	_, _ = frame.Write(payload)
	if err := group.WriteFrame(frame); err != nil {
		group.CancelWrite(moqt.InternalGroupErrorCode)
		tw.CloseWithError(moqt.InternalSubscribeErrorCode)
		return
	}
	if err := group.Close(); err != nil {
		tw.CloseWithError(moqt.InternalSubscribeErrorCode)
		return
	}
	_ = tw.Close()
}

// validateTrackHandler performs a small sanity check on a TrackHandler
// argument. It ensures that the value and, if it is a TrackHandlerFunc, the
// underlying function are not nil.
func validateTrackHandler(handler moqt.TrackHandler) error {
	if handler == nil {
		return fmt.Errorf("msf: track handler cannot be nil")
	}
	if f, ok := handler.(moqt.TrackHandlerFunc); ok && f == nil {
		return fmt.Errorf("msf: track handler function cannot be nil")
	}
	return nil
}

// staleTrackNamesLocked reports track names present in the current catalog but absent from catalog.
func (b *Broadcast) staleTrackNamesLocked(catalog Catalog) []moqt.TrackName {
	if len(b.catalog.Tracks) == 0 {
		return nil
	}

	activeNames := make(map[moqt.TrackName]struct{}, len(catalog.Tracks))
	for _, track := range catalog.Tracks {
		activeNames[moqt.TrackName(track.Name)] = struct{}{}
	}

	stale := make([]moqt.TrackName, 0)
	for _, track := range b.catalog.Tracks {
		name := moqt.TrackName(track.Name)
		if _, ok := activeNames[name]; ok {
			continue
		}
		stale = append(stale, name)
	}

	return stale
}

// validateCatalogForBroadcast rejects catalog shapes that cannot be routed by Broadcast.
func validateCatalogForBroadcast(catalog Catalog, catalogTrackName moqt.TrackName) error {
	seen := make(map[moqt.TrackName]TrackID, len(catalog.Tracks))
	for _, track := range catalog.Tracks {
		name := moqt.TrackName(track.Name)
		if name == catalogTrackName {
			return fmt.Errorf("msf: catalog contains reserved track name %q", catalogTrackName)
		}
		id := track.ID(catalog.DefaultNamespace)
		if previous, ok := seen[name]; ok && previous != id {
			return fmt.Errorf("msf: broadcast requires unique track names across namespaces; duplicate name %q found for %q and %q", name, previous.String(), id.String())
		}
		seen[name] = id
	}
	return nil
}
