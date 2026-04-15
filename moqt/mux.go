package moqt

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"sync"
)

// DefaultMux is the package-level TrackMux used by convenience top-level functions such as
// Publish and Announce. It provides a global multiplexer suitable for simple server
// implementations.
var DefaultMux *TrackMux = defaultMux

var defaultMux = NewTrackMux(0)

// HopID returns the hop identifier configured for this TrackMux.
func (mux *TrackMux) HopID() uint64 {
	return mux.hopID
}

// NewHopID generates a cryptographically random non-zero hop identifier.
// Relay nodes SHOULD call this to obtain a unique ID for NewTrackMux.
func NewHopID() uint64 {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			panic("moqt: crypto/rand unavailable: " + err.Error())
		}
		if id := binary.BigEndian.Uint64(b[:]); id != 0 {
			return id
		}
	}
}

// NewTrackMux creates a new TrackMux with the given hop identifier.
// Pass id = 0 for edge nodes (origin publishers or pure subscribers);
// per the moq-lite spec, 0 indicates an unknown or bridged hop.
// Relay nodes MUST pass a unique non-zero id — use NewHopID() to generate one.
func NewTrackMux(id uint64) *TrackMux {
	return &TrackMux{
		hopID: id,
		announcementTree: announcingNode{
			children:      make(map[prefixSegment]*announcingNode),
			subscriptions: make(map[*AnnouncementWriter](chan *Announcement)),
			announcements: make(map[*Announcement]struct{}),
		},
		// Pre-allocate with reasonable capacity to reduce map growth
		trackHandlerIndex: make(map[BroadcastPath]*announcedTrackHandler, 16),
	}
}

// Publish registers the handler for the given track path in the DefaultMux.
// The handler remains active until the provided context is canceled.
// This is a convenience wrapper around DefaultMux.Publish.
func Publish(ctx context.Context, path BroadcastPath, handler TrackHandler) {
	DefaultMux.Publish(ctx, path, handler)
}

// PublishFunc is a convenience wrapper that registers a simple handler
// function for the given track path on the DefaultMux.
func PublishFunc(ctx context.Context, path BroadcastPath, f func(tw *TrackWriter)) {
	DefaultMux.PublishFunc(ctx, path, f)
}

// Announce registers an Announcement and associated handler in the
// DefaultMux. It is used to publish an Announcement object alongside
// a TrackHandler that will serve any subscribers of the announced path.
func Announce(announcement *Announcement, handler TrackHandler) {
	DefaultMux.Announce(announcement, handler)
}

// TrackMux routes announcements and track subscriptions to the correct TrackHandler.
// It keeps an index of broadcast paths to handlers and an announcement tree that
// notifies listeners matching a broadcast-path prefix.
type TrackMux struct {
	// hopID is a unique identifier for this node in the relay path.
	// Each relay MUST set a non-zero hopID. A value of 0 means this node
	// does not participate in hop tracking (e.g. an endpoint, not a relay).
	hopID uint64

	mu                sync.RWMutex
	trackHandlerIndex map[BroadcastPath]*announcedTrackHandler

	announcementTree announcingNode
	// treeMu           sync.RWMutex
}

// PublishFunc registers a simple function handler for the provided path on
// the TrackMux. It wraps the function into a TrackHandlerFunc.
func (mux *TrackMux) PublishFunc(ctx context.Context, path BroadcastPath, f func(tw *TrackWriter)) {
	mux.Publish(ctx, path, TrackHandlerFunc(f))
}

// Publish registers the handler for the given track path on the TrackMux.
// The handler remains active until the provided context is canceled.
func (mux *TrackMux) Publish(ctx context.Context, path BroadcastPath, handler TrackHandler) {
	if ctx == nil {
		panic("[TrackMux] nil context")
	}

	if !isValidPath(path) {
		panic("[TrackMux] invalid track path: " + path)
	}
	ann, _ := NewAnnouncement(ctx, path)
	mux.Announce(ann, handler)
}

func (mux *TrackMux) registerHandler(ann *Announcement, handler TrackHandler) *announcedTrackHandler {
	path := ann.BroadcastPath()

	// Allocate new handler outside lock to reduce lock hold time
	newHandler := &announcedTrackHandler{
		Announcement: ann,
		TrackHandler: handler,
	}

	mux.mu.Lock()
	announced, ok := mux.trackHandlerIndex[path]
	mux.trackHandlerIndex[path] = newHandler
	mux.mu.Unlock()

	// Cleanup old handler outside lock
	if ok {
		announced.end()
	}

	return newHandler
}

func (mux *TrackMux) removeHandler(handler *announcedTrackHandler) {
	path := handler.BroadcastPath()
	mux.mu.Lock()
	defer mux.mu.Unlock()
	if ath, ok := mux.trackHandlerIndex[path]; ok && ath == handler {
		delete(mux.trackHandlerIndex, path)
	}
}

func (mux *TrackMux) Announce(announcement *Announcement, handler TrackHandler) {
	if announcement == nil {
		panic("[TrackMux] nil announcement")
	}

	if !announcement.IsActive() {
		return
	}

	// Add the handler to the mux if it is not already registered
	announced := mux.registerHandler(announcement, handler)

	prefixSegments, _ := pathSegments(announcement.BroadcastPath())

	// Add announcement to the announcement tree so that init captures it
	current := &mux.announcementTree

	// Build a list of nodes from root to leaf and add the announcement to each node
	// Use fixed-size array for typical depths to avoid heap allocation
	var nodesArray [8]*announcingNode
	var nodes []*announcingNode
	if len(prefixSegments)+1 <= len(nodesArray) {
		nodes = nodesArray[:0]
	} else {
		nodes = make([]*announcingNode, 0, len(prefixSegments)+1)
	}
	nodes = append(nodes, current)
	for _, seg := range prefixSegments {
		current = current.getChild(seg)
		nodes = append(nodes, current)
	}

	// Reserve a subscription slice to reuse across nodes and avoid allocations
	type awChan struct {
		aw *AnnouncementWriter
		ch chan *Announcement
	}
	var subsArray [8]awChan
	subs := subsArray[:0]

	for _, node := range nodes {
		node.addAnnouncement(announcement)

		// Snapshot subscriptions under RLock and send without holding the lock
		node.mu.RLock()
		subs = subs[:0]
		for aw, ch := range node.subscriptions {
			subs = append(subs, awChan{aw: aw, ch: ch})
		}
		node.mu.RUnlock()

		for _, ac := range subs {
			ch := ac.ch
			// Non-blocking send to avoid deadlocks; drop if buffer is full
			select {
			case ch <- announcement:
			case <-announcement.Done():
			default:
				// Drop the message if the subscriber is not keeping up. Remove subscription
				// from the node and close the channel safely to signal the writer side.
				// delete by AnnouncementWriter pointer without scanning
				node.mu.Lock()
				delete(node.subscriptions, ac.aw)
				node.mu.Unlock()
				// Close the AW to signal the writer to cleanup and close its channel.
				go func(a *AnnouncementWriter) {
					// Use InternalAnnounceErrorCode to indicate an internal error condition
					_ = a.CloseWithError(AnnounceErrorCodeInternal)
				}(ac.aw)
			}
		}
	}

	lastNode := current

	// Ensure the announcement is removed when it ends
	announcement.AfterFunc(func() {
		// Remove the announcement from the tree unconditionally
		lastNode.removeAnnouncement(announcement)

		mux.removeHandler(announced)
	})
}

// TrackHandler returns the Announcement and associated TrackHandler for the specified
// broadcast path. If no handler is found, it returns nil and NotFoundTrackHandler.
func (mux *TrackMux) TrackHandler(path BroadcastPath) (*Announcement, TrackHandler) {
	ath := mux.findTrackHandler(path)
	if ath == nil {
		return nil, NotFoundTrackHandler
	}
	return ath.Announcement, ath.TrackHandler
}

func (mux *TrackMux) findTrackHandler(path BroadcastPath) *announcedTrackHandler {
	// Fast path: single RLock with minimal work under lock
	mux.mu.RLock()
	ath := mux.trackHandlerIndex[path]
	mux.mu.RUnlock()

	// Quick validation: check if path is valid and handler exists
	// Combine nil check with length check to reduce branches
	if ath == nil || len(path) == 0 || path[0] != '/' {
		return nil
	}

	// Validate handler is usable
	if ath.Announcement == nil || ath.TrackHandler == nil {
		return nil
	}

	// Rare case: treat typed-nil handler functions as absent
	if hf, ok := ath.TrackHandler.(TrackHandlerFunc); ok && hf == nil {
		return nil
	}

	return ath
}

// serveTrack serves the track at the specified path using the registered handler.
func (mux *TrackMux) serveTrack(tw *TrackWriter) {
	if tw == nil {
		return
	}

	path := tw.BroadcastPath

	// Use findTrackHandler for consistent lookup with optimized locking
	ath := mux.findTrackHandler(path)
	if ath == nil {
		tw.CloseWithError(SubscribeErrorCodeNotFound)
		return
	}

	// Ensure track is closed when announcement ends
	stop := ath.Announcement.AfterFunc(func() {
		tw.Close()
	})
	defer stop()

	ath.TrackHandler.ServeTrack(tw)
}

// serveAnnouncements serves announcements for tracks matching the given pattern.
// It registers the AnnouncementWriter and sends announcements for matching tracks.
func (mux *TrackMux) serveAnnouncements(aw *AnnouncementWriter) {
	if aw == nil {
		return
	}

	if !isValidPrefix(aw.prefix) {
		_ = aw.CloseWithError(AnnounceErrorCodeInvalidPrefix)
		return
	}

	// The AnnouncementWriter lifecycle is managed by the session/creator;
	// Do not close the writer here to avoid double-close semantics and to let
	// the caller (e.g. session) decide when to close the stream.

	// Register the handler on the routing tree (protect children with per-leafNode lock)
	leafNode := mux.announcementTree.createNode(prefixSegments(aw.prefix))

	leafNode.mu.Lock()

	// Snapshot current active announcements
	actives := make(map[*Announcement]struct{})
	for ann := range leafNode.announcements {
		actives[ann] = struct{}{}
	}

	// Channel to receive announcements
	ch := make(chan *Announcement, 8) // TODO: configurable buffer size
	if leafNode.subscriptions == nil {
		leafNode.subscriptions = make(map[*AnnouncementWriter](chan *Announcement))
	}
	leafNode.subscriptions[aw] = ch
	leafNode.mu.Unlock()

	defer func() {
		leafNode.mu.Lock()
		delete(leafNode.subscriptions, aw)
		leafNode.mu.Unlock()
	}()

	err := aw.init(actives)
	if err != nil {
		_ = aw.CloseWithError(AnnounceErrorCodeInternal)
		return
	}

	// Process announcements and exit when writer context is cancelled
	for {
		select {
		case ann, ok := <-ch:
			if !ok {
				return
			}
			if err := aw.SendAnnouncement(ann); err != nil {
				_ = aw.CloseWithError(AnnounceErrorCodeInternal)
				return
			}
		case <-aw.Context().Done():
			return
		}
	}
}

type prefixSegment = string

type announcingNode struct {
	mu sync.RWMutex

	parent *announcingNode

	prefixSegment prefixSegment

	children map[prefixSegment]*announcingNode

	subscriptions map[*AnnouncementWriter](chan *Announcement)

	announcements map[*Announcement]struct{}
}

func (node *announcingNode) getChild(seg prefixSegment) *announcingNode {
	// Fast path: check with read lock first
	node.mu.RLock()
	child := node.children[seg]
	node.mu.RUnlock()

	if child != nil {
		return child
	}

	// Slow path: create new child with write lock
	node.mu.Lock()
	// Double-check after acquiring write lock (another goroutine might have created it)
	if child = node.children[seg]; child == nil {
		child = &announcingNode{
			parent:        node,
			prefixSegment: seg,
			children:      make(map[prefixSegment]*announcingNode),
			subscriptions: make(map[*AnnouncementWriter](chan *Announcement)),
			announcements: make(map[*Announcement]struct{}),
		}
		node.children[seg] = child
	}
	node.mu.Unlock()
	return child
}

// addAnnouncement adds an announcement to the tree by traversing from parent to child nodes.
func (node *announcingNode) addAnnouncement(announcement *Announcement) {
	node.mu.Lock()
	node.announcements[announcement] = struct{}{}
	node.mu.Unlock()
}

// removeAnnouncement removes an announcement from the tree by traversing from child to parent nodes.
func (node *announcingNode) removeAnnouncement(announcement *Announcement) {
	node.mu.Lock()
	delete(node.announcements, announcement)
	shouldRemove := len(node.announcements) == 0 && len(node.children) == 0
	node.mu.Unlock()

	if shouldRemove && node.parent != nil {
		node.parent.mu.Lock()
		delete(node.parent.children, node.prefixSegment)
		node.parent.mu.Unlock()
		node.parent.removeAnnouncement(announcement)
	}
}

func (node *announcingNode) createNode(segments []prefixSegment) *announcingNode {
	if len(segments) == 0 {
		return node
	}

	child := node.getChild(segments[0])

	return child.createNode(segments[1:])
}

func isValidPath(path BroadcastPath) bool {
	// Optimize common case: check length and first char in one go
	return len(path) > 0 && path[0] == '/'
}

func isValidPrefix(prefix string) bool {
	// Optimize: check length first, then chars
	// Special case: "/" is valid as root prefix
	n := len(prefix)
	return n > 0 && prefix[0] == '/' && (n == 1 || prefix[n-1] == '/')
}

// TrackHandler handles a published track.
// Implementations will be invoked when a subscriber requests a track and are provided with a
// TrackWriter to send group frames for that track.
type TrackHandler interface {
	ServeTrack(*TrackWriter)
}

// NotFound is a default convenience handler function which responds to
// subscribers by closing the track writer with a TrackNotFound error.
var NotFound = func(tw *TrackWriter) {
	if tw == nil {
		return
	}

	tw.CloseWithError(SubscribeErrorCodeNotFound)
}

// NotFoundTrackHandler is a TrackHandler that implements a not-found
// behavior by calling NotFound handler.
var NotFoundTrackHandler TrackHandler = TrackHandlerFunc(NotFound)

// TrackHandlerFunc is an adapter to allow ordinary functions to act as a
// TrackHandler. It implements the TrackHandler interface.
type TrackHandlerFunc func(*TrackWriter)

func (f TrackHandlerFunc) ServeTrack(tw *TrackWriter) {
	f(tw)
}

var _ TrackHandler = (*announcedTrackHandler)(nil)

type announcedTrackHandler struct {
	TrackHandler
	*Announcement
}

func prefixSegments(prefix string) []prefixSegment {
	// For typical paths like "/a/b/c/", skip first and last empty segments
	if len(prefix) <= 2 {
		if prefix == "/" {
			return []prefixSegment{} // Return empty slice, not nil
		}
		return nil
	}

	// Manual scanning to avoid strings.Split allocation
	// Need to preserve empty segments to match original behavior
	str := prefix[1 : len(prefix)-1]        // Skip leading and trailing slashes
	segments := make([]prefixSegment, 0, 8) // Pre-allocate for typical depth
	start := 0
	for i := 0; i < len(str); i++ {
		if str[i] == '/' {
			segments = append(segments, str[start:i]) // Include empty strings
			start = i + 1
		}
	}
	// Add last segment (always, even if empty)
	segments = append(segments, str[start:])
	return segments
}

func pathSegments(path BroadcastPath) (prefixSegments []prefixSegment, last string) {
	p := string(path)
	if len(p) <= 1 {
		if p == "/" {
			return []prefixSegment{}, "" // Root case: empty slice and empty string
		}
		return nil, p
	}

	// Count slashes to pre-allocate exact size and avoid allocations
	str := p[1:] // Skip leading slash
	slashCount := 0
	for i := 0; i < len(str); i++ {
		if str[i] == '/' {
			slashCount++
		}
	}

	// Allocate exact size needed
	segments := make([]string, 0, slashCount+1)
	start := 0
	for i := 0; i < len(str); i++ {
		if str[i] == '/' {
			segments = append(segments, str[start:i])
			start = i + 1
		}
	}
	// Add last segment
	segments = append(segments, str[start:])

	if len(segments) == 0 {
		return nil, p
	}
	if len(segments) == 1 {
		return nil, segments[0]
	}
	return segments[:len(segments)-1], segments[len(segments)-1]
}
