package moqt

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// EndAnnouncementFunc is a function that ends an announcement.
type EndAnnouncementFunc func()

// NewAnnouncement constructs a new Announcement for the given broadcast path.
//
// The returned *Announcement remains active until the provided context is canceled or the
// returned EndAnnouncementFunc is called.
// When the announcement ends, its Done() channel is closed once and all callbacks registered via
// AfterFunc are executed exactly once.
// If ctx is already canceled, the Announcement is initially inactive.
func NewAnnouncement(ctx context.Context, path BroadcastPath) (*Announcement, EndAnnouncementFunc) {
	if !isValidPath(path) {
		panic("[Announcement] invalid track path: " + string(path))
	}

	ann := &Announcement{
		path: path,
		ch:   make(chan struct{}),
	}
	if ctx.Err() != nil {
		ann.active.Store(false)
	} else {
		ann.active.Store(true)
	}
	endFunc := func() { ann.end() }

	context.AfterFunc(ctx, endFunc)

	return ann, endFunc
}

// Announcement represents the lifecycle of a broadcast.
//
// The key behaviors are:
// - Done() returns a channel that is closed once when the announcement ends.
// - AfterFunc registers a callback to be invoked once when the announcement ends.
// - If the announcement has already ended, the callback is invoked synchronously.
// - The stop function (returned by AfterFunc) removes the callback if it hasn't executed yet and returns true; otherwise it returns false.
//
// Methods are safe to call concurrently.
// AfterFunc and the returned stop function are safe to call concurrently with end().
// end() is idempotent and handlers execute at most once.
type Announcement struct {
	mu sync.Mutex
	ch chan struct{}

	path BroadcastPath

	afterHandlers []func()

	active atomic.Bool
	once   sync.Once

	hopIDs []uint64
}

// String returns a string representation of the announcement for debugging.
func (a *Announcement) String() string {
	var sb strings.Builder
	sb.WriteString("{")
	sb.WriteString(" ")
	sb.WriteString("announce_status: ")
	if a.IsActive() {
		sb.WriteString("active")
	} else {
		sb.WriteString("ended")
	}
	sb.WriteString(", ")
	sb.WriteString("broadcast_path: ")
	sb.WriteString(a.path.String())
	sb.WriteString(" }")
	return sb.String()
}

// BroadcastPath returns the broadcast path associated with the announcement.
// The returned path is immutable for the lifetime of the announcement and is
// safe to copy.
func (a *Announcement) BroadcastPath() BroadcastPath {
	return a.path
}

// HopIDs returns the list of hop IDs the announcement has traversed.
// Each relay appends its own hop ID when forwarding. For local announcements, HopIDs returns nil.
func (a *Announcement) HopIDs() []uint64 {
	return a.hopIDs
}

// Done returns a channel that is closed once when the announcement ends.
// Consumers may use this channel to wait until the announcement stops.
func (a *Announcement) Done() <-chan struct{} {
	return a.ch
}

// AfterFunc registers a callback f to be invoked once when the announcement
// ends. If the announcement is active, f is queued and a stop function is
// returned that removes the registration (returns true when removed).
// If the announcement already ended, f is called asynchronously in a new goroutine
// and the stop function always returns false. AfterFunc and the returned stop
// function are safe to call concurrently with end().
func (a *Announcement) AfterFunc(f func()) (stop func() bool) {
	// Synchronize access to afterHandlers to avoid races with end()
	a.mu.Lock()
	if !a.active.Load() {
		a.mu.Unlock()
		// Announcement already ended — call in a goroutine to avoid deadlock
		go f()
		return func() bool { return false }
	}

	if a.afterHandlers == nil {
		// Pre-allocate with small capacity to reduce growth
		a.afterHandlers = make([]func(), 0, 2)
	}
	index := len(a.afterHandlers)
	a.afterHandlers = append(a.afterHandlers, f)
	a.mu.Unlock()

	// Create stopFunc that marks the handler as nil instead of removing it
	// This avoids slice reallocation and index shifting
	return func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.afterHandlers == nil || index >= len(a.afterHandlers) {
			return false
		}
		if a.afterHandlers[index] != nil {
			a.afterHandlers[index] = nil
			return true
		}
		return false
	}
}

// IsActive reports whether the announcement is currently active.
// Returns false once the announcement has ended.
func (a *Announcement) IsActive() bool {
	return a.active.Load()
}

func (a *Announcement) end() {
	// Ensure end() body only runs once
	a.once.Do(func() {
		// set active to false
		a.active.Store(false)

		// Snapshot handlers under lock, then clear to avoid double-execution and races
		a.mu.Lock()
		handlers := a.afterHandlers
		a.afterHandlers = nil
		a.mu.Unlock()

		// Count non-nil handlers (ranging over nil slice is safe)
		handlerCount := 0
		for _, h := range handlers {
			if h != nil {
				handlerCount++
			}
		}

		// Fast path for small number of handlers
		if handlerCount <= 2 {
			// Execute handlers inline for small counts to avoid goroutine overhead
			for _, handler := range handlers {
				if handler != nil {
					handler()
				}
			}
		} else {
			// Use worker pool for larger handler counts
			// Limit workers to avoid excessive goroutine creation
			workerCount := min(runtime.NumCPU(), handlerCount)

			// Pre-allocate slice to avoid channel allocation overhead
			handlerSlice := make([]func(), 0, handlerCount)
			for _, handler := range handlers {
				if handler != nil {
					handlerSlice = append(handlerSlice, handler)
				}
			}

			var wg sync.WaitGroup
			wg.Add(handlerCount)

			// Distribute work among workers
			chunkSize := (handlerCount + workerCount - 1) / workerCount
			for i := range workerCount {
				start := i * chunkSize
				end := min(start+chunkSize, handlerCount)

				if start >= handlerCount {
					break
				}

				go func(handlers []func()) {
					for _, h := range handlers {
						h()
						wg.Done()
					}
				}(handlerSlice[start:end])
			}

			wg.Wait()
		}

		// Close the Done channel once (since the once.Do wraps this function)
		close(a.ch)
	})
}
