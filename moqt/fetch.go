package moqt

import (
	"context"
	"errors"
	"fmt"
)

// FetchRequest asks for one group of a track.
type FetchRequest struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName
	Priority      TrackPriority
	GroupSequence GroupSequence

	// frameStart and frameEnd bound the frames to return within the group
	// (moq-lite-06 §7.16), in their wire form: frameStart is the first index,
	// frameEnd the exclusive end (0 through the end of the group). They are
	// set only from a peer's FETCH, whose GroupWriter then sends just those
	// frames; a fetch this endpoint makes asks for the whole group.
	frameStart uint64
	frameEnd   uint64

	ctx context.Context
}

// errFetchFrameRange is returned for a FETCH whose Frame End is not above its
// Frame Start, which moq-lite-06 §7.16 makes a protocol violation.
var errFetchFrameRange = errors.New("moqt: fetch Frame End must be above Frame Start")

// validate checks the frame range rule of moq-lite-06 §7.16.
func (r *FetchRequest) validate() error {
	if r.frameEnd != 0 && r.frameEnd <= r.frameStart {
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
