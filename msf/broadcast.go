package msf

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt"
)

// DefaultCatalogTrackName is the Track Name the top-level MSF catalog track
// MUST have (draft-ietf-moq-msf-02 §5). Catalogs referenced from it through
// packaging "catalog" tracks may use other names.
const DefaultCatalogTrackName moqt.TrackName = "catalog"

// ErrTrackNotFound reports that Broadcast.RemoveTrack found no track or
// handler with the given name.
var ErrTrackNotFound = errors.New("msf: track not found")

// Broadcast is an MSF-aware moqt.TrackHandler that serves the catalog track and
// routes other track subscriptions to registered per-track handlers.
//
// It keeps the current catalog snapshot and handler table together so callers
// can manage a broadcast through a single object. Track replacement and
// removal close any active TrackWriter values for that track; handlers are
// expected to return once their writer is closed.
//
// A catalog subscription stays open for the life of the broadcast: the
// subscriber receives the current catalog as its first group, and a new group
// with the whole catalog each time SetCatalog, RegisterTrack or RemoveTrack
// changes it. Changes made while a group is being written are combined into
// the next one, so a slow subscriber skips intermediate catalogs rather than
// holding up the broadcast. Close ends every catalog subscription.
//
// Because the underlying moqt routing key is a single TrackName, Broadcast
// requires track names to be unique across namespaces within the current
// catalog snapshot. This is a routing constraint of Broadcast, stricter than
// the MSF rule that only Full Track Names (namespace and name) be unique,
// which Catalog.Validate enforces. Catalogs that contain the same track name
// in multiple namespaces are rejected rather than being mapped to synthetic
// routing names, and no catalog track may use the catalog track's own name.
type Broadcast struct {
	mu sync.RWMutex

	catalogTrackName moqt.TrackName
	catalog          Catalog
	// payload is catalog encoded for the catalog track, or nil until a
	// catalog subscriber next needs it; it is never modified once set.
	payload []byte
	// changed is closed and replaced each time the catalog changes, waking
	// every catalog subscriber at once.
	changed chan struct{}
	closed  bool
	tracks  moqt.Broadcast
}

// initLocked initializes lazily allocated broadcast defaults while b.mu is held.
func (b *Broadcast) initLocked() {
	if b.catalogTrackName == "" {
		b.catalogTrackName = DefaultCatalogTrackName
	}
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
}

// catalogChangedLocked wakes the catalog subscribers while b.mu is held.
func (b *Broadcast) catalogChangedLocked() {
	b.payload = nil
	if b.closed {
		return
	}
	close(b.changed)
	b.changed = make(chan struct{})
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
	b.catalogChangedLocked()
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
//
// Because the catalog must be valid after every change, tracks register in
// dependency order: a track whose depends names another track or a render
// group, or whose initRef names an initDataList entry, registers after what
// it references exists (initDataList entries come from SetCatalog). To add
// tracks that reference each other at once, use SetCatalog.
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

	if err := b.tracks.Register(moqt.TrackName(trackClone.Name), handler); err != nil {
		return err
	}
	b.catalog = updated
	b.catalogChangedLocked()
	return nil
}

// RemoveTrack removes the named track and its handler from the broadcast.
//
// The broadcast's catalog stays valid: if another track still references the
// track (a depends entry naming it, or the last track of a render group that
// a depends entry names), RemoveTrack returns the *ValidationError and
// changes nothing, track and handler included. Remove the dependent tracks
// first, or replace the catalog with SetCatalog.
//
// It returns ErrTrackNotFound when neither the catalog nor the handler table
// holds the name, and an error for an empty name or the catalog track's name.
func (b *Broadcast) RemoveTrack(name moqt.TrackName) error {
	if b == nil {
		return fmt.Errorf("msf: nil broadcast")
	}
	if name == "" {
		return fmt.Errorf("msf: track name is required")
	}
	if name == b.CatalogTrackName() {
		return fmt.Errorf("msf: %q is reserved for the catalog track", name)
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

	if removed {
		if err := updated.Validate(); err != nil {
			return fmt.Errorf("msf: cannot remove track %q: %w", name, err)
		}
		b.catalog = updated
		b.catalogChangedLocked()
	}
	if !b.tracks.Remove(name) && !removed {
		return fmt.Errorf("%w: %q", ErrTrackNotFound, name)
	}
	return nil
}

// Close ends every catalog subscription and closes the TrackWriter values the
// registered track handlers are serving. The catalog is kept, but later
// catalog subscriptions end after their first group. Close is idempotent.
func (b *Broadcast) Close() {
	if b == nil {
		return
	}

	b.mu.Lock()
	b.initLocked()
	if !b.closed {
		b.closed = true
		close(b.changed)
	}
	b.mu.Unlock()

	b.tracks.Close()
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

// serveCatalogTrack writes the current catalog onto the reserved catalog
// track, then a new group each time the catalog changes, until the subscriber
// leaves or the broadcast is closed.
func (b *Broadcast) serveCatalogTrack(tw *moqt.TrackWriter) {
	ctx := tw.Context()
	var sent []byte
	for {
		payload, changed, closed, err := b.catalogState()
		if err != nil {
			tw.CloseWithError(moqt.SubscribeErrorCodeInternal)
			return
		}
		// A change that leaves the encoded catalog as it was, such as
		// SetCatalog with the same catalog, sends nothing.
		if sent == nil || !bytes.Equal(payload, sent) {
			if err := writeCatalogGroup(tw, payload); err != nil {
				if ctx.Err() != nil {
					return // the subscriber left mid-write
				}
				tw.CloseWithError(moqt.SubscribeErrorCodeInternal)
				return
			}
			sent = payload
		}
		if closed {
			_ = tw.Close() // not actionable: the subscription is over either way
			return
		}

		select {
		case <-changed:
		case <-ctx.Done():
			_ = tw.Close() // not actionable: the subscriber is gone
			return
		}
	}
}

// catalogState returns the current catalog encoded for the catalog track, the
// channel closed at its next change, and whether the broadcast is closed. The
// catalog is encoded once per change, by the first subscriber to need it.
func (b *Broadcast) catalogState() ([]byte, <-chan struct{}, bool, error) {
	b.mu.RLock()
	payload, changed, closed := b.payload, b.changed, b.closed
	b.mu.RUnlock()
	if payload != nil && changed != nil {
		return payload, changed, closed, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.initLocked()
	if b.payload == nil {
		encoded, err := b.catalog.MarshalJSON()
		if err != nil {
			return nil, nil, false, fmt.Errorf("msf: encode catalog: %w", err)
		}
		b.payload = encoded
	}
	return b.payload, b.changed, b.closed, nil
}

// writeCatalogGroup writes payload to tw as one group of one frame.
func writeCatalogGroup(tw *moqt.TrackWriter, payload []byte) error {
	group, err := tw.OpenGroup(tw.Context())
	if err != nil {
		return fmt.Errorf("msf: open catalog group: %w", err)
	}
	frame := moqt.NewFrame(len(payload))
	_, _ = frame.Write(payload) // not actionable: a Frame grows to fit
	if err := group.WriteFrame(frame); err != nil {
		group.CancelWrite(moqt.InternalGroupErrorCode)
		return fmt.Errorf("msf: write catalog group: %w", err)
	}
	if err := group.Close(); err != nil {
		return fmt.Errorf("msf: close catalog group: %w", err)
	}
	return nil
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

	stale := make([]moqt.TrackName, 0)

	// ⚡ Bolt: Fast path for small catalogs (N<=16) avoids map initialization
	// and hashing overhead by using an O(N^2) loop over a stack array.
	if len(catalog.Tracks) <= 16 {
		var activeNames [16]moqt.TrackName
		numNames := 0
		for _, track := range catalog.Tracks {
			activeNames[numNames] = moqt.TrackName(track.Name)
			numNames++
		}
		for _, track := range b.catalog.Tracks {
			name := moqt.TrackName(track.Name)
			isActive := false
			for j := 0; j < numNames; j++ {
				if activeNames[j] == name {
					isActive = true
					break
				}
			}
			if !isActive {
				stale = append(stale, name)
			}
		}
	} else {
		activeNames := make(map[moqt.TrackName]struct{}, len(catalog.Tracks))
		for _, track := range catalog.Tracks {
			activeNames[moqt.TrackName(track.Name)] = struct{}{}
		}
		for _, track := range b.catalog.Tracks {
			name := moqt.TrackName(track.Name)
			if _, ok := activeNames[name]; ok {
				continue
			}
			stale = append(stale, name)
		}
	}

	return stale
}

// validateCatalogForBroadcast rejects catalog shapes that cannot be routed by Broadcast.
func validateCatalogForBroadcast(catalog Catalog, catalogTrackName moqt.TrackName) error {
	// ⚡ Bolt: Fast path for small catalogs (N<=16) avoids map initialization
	// and hashing overhead by using an O(N^2) loop over a stack array.
	if len(catalog.Tracks) <= 16 {
		type seenEntry struct {
			name moqt.TrackName
			id   TrackID
		}
		var seen [16]seenEntry
		numSeen := 0
		for _, track := range catalog.Tracks {
			name := moqt.TrackName(track.Name)
			if name == catalogTrackName {
				return fmt.Errorf("msf: catalog contains reserved track name %q", catalogTrackName)
			}
			id := track.ID(catalog.DefaultNamespace)
			for j := 0; j < numSeen; j++ {
				if seen[j].name == name && seen[j].id != id {
					return fmt.Errorf("msf: broadcast requires unique track names across namespaces; duplicate name %q found for %q and %q", name, seen[j].id.String(), id.String())
				}
			}
			seen[numSeen] = seenEntry{name: name, id: id}
			numSeen++
		}
	} else {
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
	}
	return nil
}
