package moqt

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/transport"
)

// A Contribute Stream is a Subscribe Stream opened from the publisher's end.
// The publisher sends CONTRIBUTE_REQUEST, naming one track of a broadcast it
// does not announce. The receiver sends SUBSCRIBE on the same stream once it
// wants the track, and nothing is delivered before that.

// ErrContributeSubscribed is returned by a second
// [ContributeResponseWriter.Subscribe].
var ErrContributeSubscribed = errors.New("moqt: contribute stream already subscribed")

// ContributeRequest is a publisher's request to contribute one track to a
// broadcast it does not announce.
type ContributeRequest struct {
	BroadcastPath BroadcastPath
	TrackName     TrackName

	ctx context.Context
}

// Context returns the request's context. To change the context, use
// [ContributeRequest.WithContext].
//
// For an incoming request it ends when the contribution does: the publisher
// closed or reset the stream, the session ended, or the handler returned.
//
// The returned context is always non-nil; it defaults to the
// background context.
func (r *ContributeRequest) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// WithContext returns a shallow copy of r with its context changed
// to ctx. The provided ctx must be non-nil.
func (r *ContributeRequest) WithContext(ctx context.Context) *ContributeRequest {
	if ctx == nil {
		panic("nil context")
	}
	r2 := new(ContributeRequest)
	*r2 = *r
	r2.ctx = ctx
	return r2
}

// A ContributeHandler responds to a request to contribute a track.
//
// It is an optional interface of the [TrackHandler] registered for a broadcast
// path. A request naming that path is served by the handler when it implements
// ContributeHandler, and is refused otherwise.
//
// ServeContribute accepts the track with w.Subscribe or refuses it with
// w.CloseWithError. Returning from ServeContribute ends the contribution.
type ContributeHandler interface {
	ServeContribute(w *ContributeResponseWriter, r *ContributeRequest)
}

// ContributeHandlerFunc adapts a function to a [ContributeHandler].
type ContributeHandlerFunc func(w *ContributeResponseWriter, r *ContributeRequest)

// ServeContribute calls f(w, r).
func (f ContributeHandlerFunc) ServeContribute(w *ContributeResponseWriter, r *ContributeRequest) {
	f(w, r)
}

// ContributeResponseWriter is how a [ContributeHandler] answers a request.
type ContributeResponseWriter struct {
	sess   *Session
	stream transport.Stream
	path   BroadcastPath
	name   TrackName

	mu     sync.Mutex
	reader *TrackReader
	used   bool
}

// Subscribe accepts the contribution: it sends SUBSCRIBE on the Contribute
// Stream and waits for the publisher's answer, as [Session.Subscribe] does on
// a stream of its own. It may be called once.
func (w *ContributeResponseWriter) Subscribe(ctx context.Context, config *SubscribeConfig) (*TrackReader, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	w.mu.Lock()
	if w.used {
		w.mu.Unlock()
		return nil, ErrContributeSubscribed
	}
	w.used = true
	w.mu.Unlock()

	if config == nil {
		config = &SubscribeConfig{}
	}
	reader, err := w.sess.subscribeOn(ctx, w.stream, w.path, w.name, config)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.reader = reader
	w.mu.Unlock()
	return reader, nil
}

// TrackInfo asks the publisher for the contributed track's properties, as
// [Session.TrackInfo] does.
func (w *ContributeResponseWriter) TrackInfo(ctx context.Context) (*PublishInfo, error) {
	return w.sess.TrackInfo(ctx, w.path, w.name)
}

// CloseWithError refuses the contribution, or ends one already subscribed,
// by resetting the Contribute Stream.
func (w *ContributeResponseWriter) CloseWithError(code SubscribeErrorCode) {
	cancelStreamWithError(w.stream, transport.StreamErrorCode(code))
}

// finish ends the subscription the handler left open.
func (w *ContributeResponseWriter) finish() {
	w.mu.Lock()
	reader := w.reader
	w.mu.Unlock()
	if reader != nil {
		_ = reader.Close()
	}
}

// handleContributeStream decodes a CONTRIBUTE_REQUEST and serves it with the
// handler registered for its broadcast path.
func (sess *Session) handleContributeStream(stream transport.Stream) {
	var crm message.ContributeRequestMessage
	if err := crm.Decode(stream); err != nil {
		sess.logError("failed to decode CONTRIBUTE_REQUEST message", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return
	}

	_, trackHandler := sess.mux.TrackHandler(BroadcastPath(crm.BroadcastPath))
	handler, ok := trackHandler.(ContributeHandler)
	if !ok {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeNotFound))
		return
	}

	ctx, cancel := sess.streamContext(stream)
	defer cancel()
	w := &ContributeResponseWriter{
		sess:   sess,
		stream: stream,
		path:   BroadcastPath(crm.BroadcastPath),
		name:   TrackName(crm.TrackName),
	}
	defer w.finish()

	if err := safeServeContribute(handler, w, &ContributeRequest{
		BroadcastPath: w.path,
		TrackName:     w.name,
		ctx:           ctx,
	}); err != nil {
		sess.logError("contribute handler error", err)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
	}
}

func safeServeContribute(handler ContributeHandler, w *ContributeResponseWriter, r *ContributeRequest) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic during contribute handling: %v", p)
		}
	}()
	handler.ServeContribute(w, r)
	return nil
}

// Contribute offers one track of a broadcast this endpoint does not announce,
// and returns its writer once the peer subscribes. ctx bounds that wait.
//
// info is the track's properties, answered to the peer's TRACK requests while
// the writer is open. If info is nil, the defaults are used.
//
// The caller closes the returned TrackWriter. If the peer refuses the
// contribution, Contribute returns a [SubscribeError].
func (sess *Session) Contribute(ctx context.Context, path BroadcastPath, name TrackName, info *PublishInfo) (*TrackWriter, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if sess.closed.Load() {
		return nil, ErrClosedSession
	}
	if !isValidPath(path) {
		return nil, fmt.Errorf("invalid broadcast path: %q", path)
	}
	if info == nil {
		info = &PublishInfo{}
	}

	stream, err := sess.conn.OpenStreamSync(ctx)
	if err != nil {
		if appErr, ok := errors.AsType[*transport.ApplicationError](err); ok {
			return nil, &SessionError{ApplicationError: appErr}
		}
		return nil, fmt.Errorf("failed to open bidirectional stream: %w", err)
	}
	// Reset the stream when ctx ends, so the read of SUBSCRIBE returns.
	stop := context.AfterFunc(ctx, func() {
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
	})
	defer stop()

	if err := message.StreamTypeContribute.Encode(stream); err != nil {
		return nil, contributeStreamError(stream, "encode stream type", err)
	}
	err = message.ContributeRequestMessage{
		BroadcastPath: string(path),
		TrackName:     string(name),
	}.Encode(stream)
	if err != nil {
		return nil, contributeStreamError(stream, "encode CONTRIBUTE_REQUEST message", err)
	}

	key := contributionKey{path: path, name: name}
	entry := sess.addContributed(key, *info)

	var sm message.SubscribeMessage
	if err := sm.Decode(stream); err != nil {
		sess.removeContributed(key, entry)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, contributeStreamError(stream, "read SUBSCRIBE", err)
	}
	if BroadcastPath(sm.BroadcastPath) != path || TrackName(sm.TrackName) != name {
		sess.removeContributed(key, entry)
		cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
		return nil, fmt.Errorf("moqt: SUBSCRIBE on a contribute stream names %q %q, want %q %q",
			sm.BroadcastPath, sm.TrackName, path, name)
	}

	track, cancelTrack := sess.newSubscriptionWriter(stream, sm)
	// Release the contribution when the caller closes the writer.
	removeWriter := track.onCloseTrackFunc
	track.onCloseTrackFunc = func() {
		removeWriter()
		sess.removeContributed(key, entry)
		cancelTrack()
	}
	return track, nil
}

// contributeStreamError reports a failed read or write on a Contribute
// Stream. A reset from the peer is its refusal.
func contributeStreamError(stream transport.Stream, op string, err error) error {
	if strErr, ok := errors.AsType[*transport.StreamError](err); ok && strErr.Remote {
		stream.CancelRead(strErr.ErrorCode)
		return &SubscribeError{StreamError: strErr}
	}
	cancelStreamWithError(stream, transport.StreamErrorCode(SubscribeErrorCodeInternal))
	return fmt.Errorf("failed to %s: %w", op, err)
}

type contributionKey struct {
	path BroadcastPath
	name TrackName
}

// contributedTrack is one track this endpoint is contributing.
type contributedTrack struct {
	info PublishInfo
}

func (sess *Session) addContributed(key contributionKey, info PublishInfo) *contributedTrack {
	entry := &contributedTrack{info: info}
	sess.contributedMu.Lock()
	defer sess.contributedMu.Unlock()
	if sess.contributed == nil {
		sess.contributed = make(map[contributionKey]*contributedTrack)
	}
	sess.contributed[key] = entry
	return entry
}

// removeContributed forgets key unless a later Contribute replaced it.
func (sess *Session) removeContributed(key contributionKey, entry *contributedTrack) {
	sess.contributedMu.Lock()
	defer sess.contributedMu.Unlock()
	if sess.contributed[key] == entry {
		delete(sess.contributed, key)
	}
}

// contributedInfo returns the properties of a track this endpoint is
// contributing.
func (sess *Session) contributedInfo(path BroadcastPath, name TrackName) (PublishInfo, bool) {
	sess.contributedMu.Lock()
	defer sess.contributedMu.Unlock()
	entry, ok := sess.contributed[contributionKey{path: path, name: name}]
	if !ok {
		return PublishInfo{}, false
	}
	return entry.info, true
}
