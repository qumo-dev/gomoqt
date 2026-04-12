package moqt

import "fmt"

type FetchRequest struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName
	Priority      TrackPriority
	GroupSequence GroupSequence
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
