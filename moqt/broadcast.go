package moqt

import (
	"fmt"
	"sync"
)

// Broadcast is a generic TrackHandler multiplexer for track names within a
// single broadcast. It routes subscriptions to per-track handlers and closes
// active TrackWriter values when a handler is removed or replaced.
//
// The zero value is ready to use.
type Broadcast struct {
	mu sync.RWMutex

	trackHandlers map[TrackName]*trackHandlerEntry
}

// NewBroadcast constructs an empty Broadcast.
func NewBroadcast() *Broadcast {
	return &Broadcast{
		trackHandlers: make(map[TrackName]*trackHandlerEntry),
	}
}

func (b *Broadcast) initLocked() {
	if b.trackHandlers == nil {
		b.trackHandlers = make(map[TrackName]*trackHandlerEntry)
	}
}

// Register associates a TrackHandler with the named track. Replacing an
// existing handler closes any active TrackWriter values served by the previous
// handler so it can shut down promptly.
func (b *Broadcast) Register(name TrackName, handler TrackHandler) error {
	if b == nil {
		return fmt.Errorf("moqt: nil broadcast")
	}
	if name == "" {
		return fmt.Errorf("moqt: track name is required")
	}
	if err := validateTrackHandler(handler); err != nil {
		return err
	}

	entry := newTrackHandlerEntry(handler)

	b.mu.Lock()
	b.initLocked()
	previous := b.trackHandlers[name]
	b.trackHandlers[name] = entry
	b.mu.Unlock()

	if previous != nil {
		previous.close()
	}

	return nil
}

// Remove removes the named track handler. It returns true if a handler was
// present. Any active TrackWriter values for that handler are closed.
func (b *Broadcast) Remove(name TrackName) bool {
	if b == nil || name == "" {
		return false
	}

	b.mu.Lock()
	entry, ok := b.trackHandlers[name]
	if ok {
		delete(b.trackHandlers, name)
	}
	b.mu.Unlock()

	if entry != nil {
		entry.close()
	}

	return ok
}

// Close removes all registered track handlers and closes any active
// TrackWriter values they are serving.
func (b *Broadcast) Close() {
	if b == nil {
		return
	}

	b.mu.Lock()
	entries := make([]*trackHandlerEntry, 0, len(b.trackHandlers))
	for name, entry := range b.trackHandlers {
		delete(b.trackHandlers, name)
		entries = append(entries, entry)
	}
	b.mu.Unlock()

	for _, entry := range entries {
		entry.close()
	}
}

// Handler returns the TrackHandler registered for the named track. If no
// handler is present, NotFoundTrackHandler is returned.
func (b *Broadcast) Handler(name TrackName) TrackHandler {
	if b == nil || name == "" {
		return NotFoundTrackHandler
	}

	b.mu.RLock()
	entry, ok := b.trackHandlers[name]
	b.mu.RUnlock()
	if ok {
		return entry
	}

	return NotFoundTrackHandler
}

// ServeTrack implements TrackHandler by dispatching to the handler registered
// for tw.TrackName.
func (b *Broadcast) ServeTrack(tw *TrackWriter) {
	if tw == nil {
		return
	}

	b.Handler(tw.TrackName).ServeTrack(tw)
}

func validateTrackHandler(handler TrackHandler) error {
	if handler == nil {
		return fmt.Errorf("moqt: track handler cannot be nil")
	}
	if f, ok := handler.(TrackHandlerFunc); ok && f == nil {
		return fmt.Errorf("moqt: track handler function cannot be nil")
	}
	return nil
}

type trackHandlerEntry struct {
	mu sync.Mutex

	handler TrackHandler
	active  map[*TrackWriter]struct{}
	stopped bool
}

func newTrackHandlerEntry(handler TrackHandler) *trackHandlerEntry {
	return &trackHandlerEntry{
		handler: handler,
		active:  make(map[*TrackWriter]struct{}),
	}
}

func (h *trackHandlerEntry) ServeTrack(tw *TrackWriter) {
	if tw == nil {
		return
	}
	if !h.trackStarted(tw) {
		tw.CloseWithError(SubscribeErrorCodeNotFound)
		return
	}
	defer h.trackEnded(tw)
	h.handler.ServeTrack(tw)
}

func (h *trackHandlerEntry) close() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	active := make([]*TrackWriter, 0, len(h.active))
	for tw := range h.active {
		active = append(active, tw)
	}
	h.active = nil
	h.mu.Unlock()

	for _, tw := range active {
		_ = tw.Close()
	}
}

func (h *trackHandlerEntry) trackStarted(tw *TrackWriter) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return false
	}
	if h.active == nil {
		h.active = make(map[*TrackWriter]struct{})
	}
	h.active[tw] = struct{}{}
	return true
}

func (h *trackHandlerEntry) trackEnded(tw *TrackWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.active, tw)
}
