package moqt

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
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
			panic(fmt.Errorf("moqt: crypto/rand unavailable: %w", err))
		}
		// Mask to 62 bits to avoid overuint62
		id := binary.BigEndian.Uint64(b[:]) & 0x3FFFFFFFFFFFFFFF
		if id != 0 {
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
	// routeLens counts the paths in trackHandlerIndex by length, and
	// routeLensDesc lists those lengths, longest first. findTrackHandler
	// tries only prefixes of those lengths, so its cost does not grow with
	// the path, which is peer input and may hold thousands of segments.
	routeLens     map[int]int
	routeLensDesc []int

	announcementTree announcingNode
}

// PublishFunc registers a simple function handler for the provided path on
// the TrackMux. It wraps the function into a TrackHandlerFunc.
func (mux *TrackMux) PublishFunc(ctx context.Context, path BroadcastPath, f func(tw *TrackWriter)) {
	mux.Publish(ctx, path, TrackHandlerFunc(f))
}

// Publish registers the handler for the given track path on the TrackMux.
// The handler remains active until the provided context is canceled.
//
// The path is announced as a route (moq-lite-06 §5.1.1): the handler also
// serves subscriptions to paths beneath it that no more specific published
// path covers, and reads the requested path from TrackWriter.BroadcastPath.
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
	if !ok {
		mux.addRouteLenLocked(len(path))
	}
	mux.mu.Unlock()

	// Cleanup old handler outside lock
	if ok {
		announced.end()
	}

	return newHandler
}

// addRouteLenLocked counts a newly published path of length n. Caller MUST
// hold mux.mu.
func (mux *TrackMux) addRouteLenLocked(n int) {
	if mux.routeLens == nil {
		mux.routeLens = make(map[int]int)
	}
	if mux.routeLens[n]++; mux.routeLens[n] == 1 {
		i, _ := slices.BinarySearchFunc(mux.routeLensDesc, n, func(e, t int) int { return t - e })
		mux.routeLensDesc = slices.Insert(mux.routeLensDesc, i, n)
	}
}

// removeRouteLenLocked uncounts a removed path of length n. Caller MUST hold
// mux.mu.
func (mux *TrackMux) removeRouteLenLocked(n int) {
	if mux.routeLens[n]--; mux.routeLens[n] > 0 {
		return
	}
	delete(mux.routeLens, n)
	if i, found := slices.BinarySearchFunc(mux.routeLensDesc, n, func(e, t int) int { return t - e }); found {
		mux.routeLensDesc = slices.Delete(mux.routeLensDesc, i, i+1)
	}
}

func (mux *TrackMux) removeHandler(handler *announcedTrackHandler) {
	path := handler.BroadcastPath()
	mux.mu.Lock()
	defer mux.mu.Unlock()
	if ath, ok := mux.trackHandlerIndex[path]; ok && ath == handler {
		delete(mux.trackHandlerIndex, path)
		mux.removeRouteLenLocked(len(path))
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

	prefixSegments, last := pathSegments(announcement.BroadcastPath())

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
	var subsArray [8]awChan
	subs := subsArray[:0]

	for _, node := range nodes {
		node.addAnnouncement(announcement)
		subs = node.notify(announcement, subs)
	}

	lastNode := current

	// The announcement is a route covering every path beneath it, so a
	// request for a prefix below it sees it too, clamped to that prefix
	// (moq-lite-06 §5.1.1). Those requests sit in the subtree under the
	// announced path: below the node named by its last segment, or, for a
	// path ending in "/", below the node the loop above ended at, whose own
	// requests it has already notified. A broadcast path rarely has a
	// subtree, so this is usually a single map miss.
	dir := current
	if last != "" {
		current.mu.RLock()
		dir = current.children[last]
		current.mu.RUnlock()
	}
	if dir != nil {
		dir.walkDescendants(func(node *announcingNode) {
			subs = node.notify(announcement, subs)
		}, last != "")
	}

	// Ensure the announcement is removed when it ends
	announcement.AfterFunc(func() {
		// Remove the announcement from the tree unconditionally
		lastNode.removeAnnouncement(announcement)

		mux.removeHandler(announced)
	})
}

// TrackHandler returns the Announcement and TrackHandler that serve path: the
// announcement of path itself, or else of the longest announced route that
// covers it (moq-lite-06 §5.1.1), so an announcement of "/live" serves
// "/live/alice/cam1" unless "/live/alice" or "/live/alice/cam1" is announced.
// If no announcement covers path, it returns nil and NotFoundTrackHandler.
func (mux *TrackMux) TrackHandler(path BroadcastPath) (*Announcement, TrackHandler) {
	ath := mux.findTrackHandler(path)
	if ath == nil {
		return nil, NotFoundTrackHandler
	}
	return ath.Announcement, ath.TrackHandler
}

// findTrackHandler returns the handler of the most specific announced route
// covering path: path itself, then the longest published route that is a
// leading run of its segments ("/a/b", "/a/", "/a", "/"). It tries only the
// lengths a published path has, so it allocates nothing and its cost does
// not grow with path.
func (mux *TrackMux) findTrackHandler(path BroadcastPath) *announcedTrackHandler {
	if len(path) == 0 || path[0] != '/' {
		return nil
	}

	mux.mu.RLock()
	defer mux.mu.RUnlock()

	if ath := usableHandler(mux.trackHandlerIndex[path]); ath != nil {
		return ath
	}
	for _, n := range mux.routeLensDesc {
		// path[:n] covers path when it ends a segment there: it ends in "/"
		// itself, or path continues with "/".
		if n >= len(path) || (path[n-1] != '/' && path[n] != '/') {
			continue
		}
		if ath := usableHandler(mux.trackHandlerIndex[path[:n]]); ath != nil {
			return ath
		}
	}
	return nil
}

// usableHandler returns ath, or nil when it cannot serve: nil, or without an
// announcement or a handler, or a typed-nil handler function.
func usableHandler(ath *announcedTrackHandler) *announcedTrackHandler {
	if ath == nil || ath.Announcement == nil || ath.TrackHandler == nil {
		return nil
	}
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

// serveAnnouncements serves the announcements under the writer's prefix.
// It registers the AnnouncementWriter and sends the matching announcements.
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

	// Routes at or above the prefix cover it too, and present clamped to it
	// (moq-lite-06 §5.1.1). They are found by exact path, one lookup per
	// level of the prefix. Taken after subscribing, so one announced in
	// between arrives on ch as well, which the writer ignores as a repeat.
	mux.coveringAnnouncements(aw.prefix, actives)

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

// coveringAnnouncements adds to dst the active announcements whose path is at
// or above pfx: the root, then each of pfx's leading segments, both without
// and with its trailing "/" ("/a" and "/a/" for "/a/b/"). pfx ends in "/".
func (mux *TrackMux) coveringAnnouncements(pfx prefix, dst map[*Announcement]struct{}) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()
	add := func(path string) {
		if ath := mux.trackHandlerIndex[BroadcastPath(path)]; ath != nil && ath.IsActive() {
			dst[ath.Announcement] = struct{}{}
		}
	}
	add("/")
	for i := 1; i < len(pfx); i++ {
		if pfx[i] == '/' {
			add(pfx[:i])
			add(pfx[:i+1])
		}
	}
}

type prefixSegment = string

// awChan is one announcement subscription of a node.
type awChan struct {
	aw *AnnouncementWriter
	ch chan *Announcement
}

// notify hands announcement to every subscription of node without blocking,
// reusing buf for the snapshot, and returns buf. A subscription that is not
// keeping up is removed and its writer closed.
func (node *announcingNode) notify(announcement *Announcement, buf []awChan) []awChan {
	// Snapshot subscriptions under RLock and send without holding the lock
	node.mu.RLock()
	buf = buf[:0]
	for aw, ch := range node.subscriptions {
		buf = append(buf, awChan{aw: aw, ch: ch})
	}
	node.mu.RUnlock()

	for _, ac := range buf {
		// Non-blocking send to avoid deadlocks; drop if buffer is full
		select {
		case ac.ch <- announcement:
		case <-announcement.Done():
		default:
			// Drop the message if the subscriber is not keeping up. Remove subscription
			// from the node and close the channel safely to signal the writer side.
			node.mu.Lock()
			delete(node.subscriptions, ac.aw)
			node.mu.Unlock()
			// Close the AW to signal the writer to cleanup and close its channel.
			go func(a *AnnouncementWriter) {
				_ = a.CloseWithError(AnnounceErrorCodeInternal)
			}(ac.aw)
		}
	}
	return buf
}

// walkDescendants calls fn for node's descendants, and for node itself when
// self is set.
func (node *announcingNode) walkDescendants(fn func(*announcingNode), self bool) {
	if self {
		fn(node)
	}
	node.mu.RLock()
	children := make([]*announcingNode, 0, len(node.children))
	for _, child := range node.children {
		children = append(children, child)
	}
	node.mu.RUnlock()
	for _, child := range children {
		child.walkDescendants(fn, true)
	}
}

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
	if len(prefix) == 0 {
		return false
	}
	// Special case: "/" is valid as root prefix
	return prefix[0] == '/' && (len(prefix) == 1 || prefix[len(prefix)-1] == '/')
}

// TrackHandler handles a published track.
// Implementations will be invoked when a subscriber requests a track and are provided with a
// TrackWriter to send group frames for that track.
type TrackHandler interface {
	ServeTrack(*TrackWriter)
}

// TrackInfoProvider is optionally implemented by TrackHandlers that can
// answer TRACK_INFO requests with a track's immutable publisher properties.
// Handlers that do not implement it are served with default properties
// (zero priority, unordered, no retention, DefaultTimescale).
type TrackInfoProvider interface {
	// TrackInfo returns the publisher properties for the named track and
	// whether the track exists.
	TrackInfo(name TrackName) (PublishInfo, bool)
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

	str := prefix[1 : len(prefix)-1] // Skip leading and trailing slashes

	n := strings.Count(str, "/")
	if n == 0 {
		return []prefixSegment{str}
	}
	if n == 1 {
		idx := strings.IndexByte(str, '/')
		return []prefixSegment{str[:idx], str[idx+1:]}
	}

	segments := make([]prefixSegment, 0, n+1) // Pre-allocate with exact depth
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

func pathSegments(path BroadcastPath) ([]prefixSegment, string) {
	p := string(path)
	if len(p) <= 1 {
		if p == "/" {
			return []prefixSegment{}, "" // Root case: empty slice and empty string
		}
		return nil, p
	}

	lastSlashIdx := strings.LastIndexByte(p, '/')
	if lastSlashIdx <= 0 {
		return nil, p[1:]
	}

	prefix := p[1:lastSlashIdx]
	last := p[lastSlashIdx+1:]

	n := strings.Count(prefix, "/")
	if n == 0 {
		return []prefixSegment{prefix}, last
	} else if n == 1 {
		idx := strings.IndexByte(prefix, '/')
		return []prefixSegment{prefix[:idx], prefix[idx+1:]}, last
	}

	segments := make([]prefixSegment, 0, n+1)
	start := 0
	for i := 0; i < len(prefix); i++ {
		if prefix[i] == '/' {
			segments = append(segments, prefix[start:i])
			start = i + 1
		}
	}
	segments = append(segments, prefix[start:])

	return segments, last
}
