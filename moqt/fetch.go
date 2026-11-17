package moqt

import (
	"context"
	"errors"
	"fmt"
)

// FetchRequest asks for one group of a track, or a run of frames within it.
type FetchRequest struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName
	Priority      TrackPriority
	GroupSequence GroupSequence

	// FrameStart is the index of the first frame to return; zero returns the
	// group from its beginning.
	FrameStart uint64
	// FrameEnd is the exclusive end of the frames to return; zero returns
	// through the end of the group. A non-zero FrameEnd must be above
	// FrameStart.
	//
	// A publisher that cannot serve the whole range resets the fetch rather
	// than return a shorter run. A group that ends before FrameEnd is not
	// short: its frames from FrameStart on are returned.
	FrameEnd uint64

	ctx context.Context
}

// errFetchFrameRange is returned for a FetchRequest whose FrameEnd is not
// above its FrameStart, which moq-lite-06 §7.16 makes a protocol violation.
var errFetchFrameRange = errors.New("moqt: fetch FrameEnd must be above FrameStart")

// validate checks the frame range rule of moq-lite-06 §7.16.
func (r *FetchRequest) validate() error {
	if r.FrameEnd != 0 && r.FrameEnd <= r.FrameStart {
		return errFetchFrameRange
	}
	return nil
}

// Context returns the request's context. To change the context, use
// [FetchRequest.WithContext].
//
// The returned context is always non-nil; it defaults to the
// background context.
func (r *FetchRequest) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// WithContext returns a shallow copy of r with its context changed
// to ctx. The provided ctx must be non-nil.
func (r *FetchRequest) WithContext(ctx context.Context) *FetchRequest {
	if ctx == nil {
		panic("nil context")
	}
	r2 := new(FetchRequest)
	*r2 = *r
	r2.ctx = ctx
	return r2
}

// Clone returns a deep copy of r with its context changed to ctx.
// The provided ctx must be non-nil.
func (r *FetchRequest) Clone(ctx context.Context) *FetchRequest {
	if ctx == nil {
		panic("nil context")
	}
	r2 := new(FetchRequest)
	*r2 = *r
	r2.ctx = ctx
	return r2
}

type FetchHandler interface {
	ServeFetch(w *GroupWriter, r *FetchRequest)
}

type FetchHandlerFunc func(w *GroupWriter, r *FetchRequest)

func (f FetchHandlerFunc) ServeFetch(w *GroupWriter, r *FetchRequest) {
	f(w, r)
}

func validateFetchHandler(handler FetchHandler) error {
	if handler == nil {
		return fmt.Errorf("moqt: fetch handler cannot be nil")
	}
	if f, ok := handler.(FetchHandlerFunc); ok && f == nil {
		return fmt.Errorf("moqt: fetch handler function cannot be nil")
	}
	return nil
}

func safeServeFetch(handler FetchHandler, w *GroupWriter, r *FetchRequest) (err error) {
	if e := validateFetchHandler(handler); e != nil {
		return e
	}

	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic during fetch handling: %v", p)
		}
	}()
	handler.ServeFetch(w, r)
	return nil
}
