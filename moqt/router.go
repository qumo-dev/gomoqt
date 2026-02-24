package moqt

import (
	"context"
	"sync"
)

// DefaultRouter is the package-level SetupRouter used by convenience
// functions Handle and HandleFunc. It provides simple routing for session
// setup requests based on path matching.
var DefaultRouter *SetupRouter = defaultRouter

var defaultRouter *SetupRouter = NewRouter()

// HandleFunc registers the given function as a SetupHandler for the path
// pattern on the DefaultRouter. The function is wrapped into a
// SetupHandlerFunc.
func HandleFunc(pattern string, f func(w SetupResponseWriter, r *SetupRequest)) {
	DefaultRouter.HandleFunc(pattern, f)
}

// Handle registers the SetupHandler for the path pattern on the
// DefaultRouter.
func Handle(pattern string, h SetupHandler) {
	DefaultRouter.Handle(pattern, h)
}

// NewRouter creates a new SetupRouter instance. Use this to construct a
// custom router instead of using DefaultRouter.
func NewRouter() *SetupRouter {
	return &SetupRouter{
		handlers: make(map[string]SetupHandler),
	}
}

// SetupRouter maps incoming setup request paths to SetupHandler handlers and provides a concurrency-safe lookup for the server setup process.
type SetupRouter struct {
	mu       sync.RWMutex
	handlers map[string]SetupHandler
}

func (r *SetupRouter) Handle(pattern string, h SetupHandler) {
	r.register(pattern, h)
}

func (r *SetupRouter) HandleFunc(pattern string, f func(w SetupResponseWriter, r *SetupRequest)) {
	r.Handle(pattern, SetupHandlerFunc(f))
}

func (r *SetupRouter) register(path string, h SetupHandler) {
	if path == "" {
		panic("moq: path cannot be empty")
	}
	if path[0] != '/' {
		panic("moq: path must start with '/'")
	}
	if h == nil {
		panic("moq: handler cannot be nil")
	}
	if f, ok := h.(SetupHandlerFunc); ok && f == nil {
		panic("moq: handler function cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]SetupHandler)
	}
	r.handlers[path] = h
}

func (r *SetupRouter) Handler(pattern string) SetupHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if h, ok := r.handlers[pattern]; ok {
		return h
	}
	return RejectSetupHandler
}

func (r *SetupRouter) ServeMOQ(w SetupResponseWriter, req *SetupRequest) {
	r.mu.RLock()
	handler := r.Handler(req.Path)
	r.mu.RUnlock()
	if handler == nil {
		handler = RejectSetupHandler
	}

	handler.ServeMOQ(w, req)
}

// SetupHandler handles setup requests coming from a client. Implementors
// receive a SetupResponseWriter to accept or reject the session and a
// SetupRequest that contains the client-provided parameters.
type SetupHandler interface {
	ServeMOQ(w SetupResponseWriter, r *SetupRequest)
}

// SetupRequest contains information about an incoming session setup
// request from a client, including the requested path, supported
// protocol versions, and client-provided extensions.
type SetupRequest struct {
	Path             string
	Versions         []Version
	ClientExtensions *Extension
	RemoteAddr       string // remote network address of the client

	ctx context.Context
}

func (r *SetupRequest) Context() context.Context {
	return r.ctx
}

// SetupResponseWriter is provided to the SetupHandler to configure the
// server response to a setup request. Handlers can select the agreed
// protocol version, provide server extensions, or reject the setup.
type SetupResponseWriter interface {
	SelectVersion(v Version) error
	SetExtensions(extensions *Extension)
	Reject(code SessionErrorCode) error
}

var _ SetupHandler = (*SetupHandlerFunc)(nil)

// SetupHandlerFunc is an adapter to allow ordinary functions to implement
// SetupHandler.
type SetupHandlerFunc func(w SetupResponseWriter, r *SetupRequest)

func (f SetupHandlerFunc) ServeMOQ(w SetupResponseWriter, r *SetupRequest) {
	f(w, r)
}

var RejectSetupFunc func(w SetupResponseWriter, r *SetupRequest) = func(w SetupResponseWriter, r *SetupRequest) {
	_ = w.Reject(SetupFailedErrorCode)
}

var RejectSetupHandler SetupHandler = SetupHandlerFunc(RejectSetupFunc)
