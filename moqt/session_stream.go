package moqt

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/okdaichi/gomoqt/quic"
)

func newSessionStream(stream quic.Stream) *sessionStream {
	ss := &sessionStream{
		ctx:       context.WithValue(stream.Context(), &biStreamTypeCtxKey, message.StreamTypeSession),
		stream:    stream,
		updatedCh: make(chan struct{}, 1),
	}
	return ss
}

type sessionStream struct {
	ctx       context.Context
	updatedCh chan struct{}

	localBitrate  uint64 // The bitrate set by the local
	remoteBitrate uint64 // The bitrate set by the remote

	stream quic.Stream

	mu sync.Mutex

	listenOnce sync.Once
}

type response struct {
	sessionStream *sessionStream
	version       Version
	extensions    *Extension
}

func newResponse(sessStr *sessionStream, version Version, extensions *Extension) *response {
	return &response{
		sessionStream: sessStr,
		version:       version,
		extensions:    extensions,
	}
}

func (ss *sessionStream) updateSession(bitrate uint64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	err := message.SessionUpdateMessage{
		Bitrate: bitrate,
	}.Encode(ss.stream)
	if err != nil {
		return Cause(ss.ctx)
	}

	ss.localBitrate = bitrate

	return nil
}

// handleUpdates triggers the goroutine to start listening for session updates
func (ss *sessionStream) handleUpdates() {
	// Safe to call multiple times
	ss.listenOnce.Do(func() {
		go func() {
			var sum message.SessionUpdateMessage
			var err error

			for {
				err = sum.Decode(ss.stream)
				if err != nil {
					break
				}

				ss.mu.Lock()
				ss.remoteBitrate = sum.Bitrate
				select {
				case ss.updatedCh <- struct{}{}:
				default:
				}
				ss.mu.Unlock()
			}

			ss.mu.Lock()
			if ss.updatedCh != nil {
				ch := ss.updatedCh
				ss.updatedCh = nil
				close(ch)
			}
			ss.mu.Unlock()
		}()
	})
}

func (ss *sessionStream) Updated() <-chan struct{} {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.updatedCh
}

func (ss *sessionStream) Context() context.Context {
	return ss.ctx
}

func (ss *sessionStream) closeWithError(code SessionErrorCode) {
	ss.mu.Lock()
	if ss.updatedCh != nil {
		ch := ss.updatedCh
		ss.updatedCh = nil
		close(ch)
	}
	ss.mu.Unlock()
	ss.stream.CancelRead(quic.StreamErrorCode(code))
	ss.stream.CancelWrite(quic.StreamErrorCode(code))
}

var _ SetupResponseWriter = (*responseWriter)(nil)

func newResponseWriter(conn quic.Connection, rsp *response, server *Server, clientVersions []Version) *responseWriter {
	return &responseWriter{
		response:       rsp,
		conn:           conn,
		server:         server,
		clientVersions: clientVersions,
	}
}

type responseWriter struct {
	// sessionStream *sessionStream
	conn      quic.Connection
	server    *Server
	onceSetup sync.Once

	clientVersions []Version
	response       *response
}

func (w *responseWriter) SetExtensions(extensions *Extension) {
	w.response.extensions = extensions
}

// SelectVersion is called by a setup handler to record which protocol
// version the server has agreed to use.  It verifies that the version was
// advertised by the client in the original SetupRequest.
func (w *responseWriter) SelectVersion(v Version) error {
	versions := w.clientVersions

	if versions == nil {
		return fmt.Errorf("no versions provided by client")
	}
	if !slices.Contains(versions, v) {
		return fmt.Errorf("version %d not supported by client", v)
	}

	return nil
}

func (w *responseWriter) Extensions() *Extension {
	return w.response.extensions
}

func (w *responseWriter) Version() Version {
	return w.response.version
}

func (w *responseWriter) Reject(code SessionErrorCode) {
	w.response.sessionStream.closeWithError(code)
}

// Accept accepts a setup request and converts it to an active Session.
// The function expects a SetupResponseWriter as provided by the server when
// responding to a client SETUP request. It uses the provided TrackMux to
// route tracks for the accepted session.
func Accept(w SetupResponseWriter, r *SetupRequest, mux *TrackMux) (*Session, error) {
	if writer, ok := w.(*responseWriter); ok {
		var err error
		rsp := writer.response
		writer.onceSetup.Do(func() {
			err = message.SessionServerMessage{
				SelectedVersion: uint64(rsp.version),
				Parameters:      rsp.extensions.parameters,
			}.Encode(rsp.sessionStream.stream)
			if err != nil {
				return
			}

			// Start listening for updates
			rsp.sessionStream.handleUpdates()
		})
		if err != nil {
			return nil, err
		}

		server := writer.server

		var sess *Session
		sess = newSession(
			writer.conn,
			rsp.sessionStream,
			mux,
			func() { server.removeSession(sess) },
		)
		server.addSession(sess)
		return sess, nil
	} else {
		return nil, fmt.Errorf("moq: invalid response writer type %T", w)
	}
}
