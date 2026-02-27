package moqt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRouter(t *testing.T) {
	r := NewRouter()
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}
	if r.handlers == nil {
		t.Fatal("handlers map not initialized")
	}
}

func TestHandleFunc(t *testing.T) {
	r := NewRouter()
	r.HandleFunc("/test", func(w SetupResponseWriter, req *SetupRequest) {
		// dummy
	})
	handler := r.Handler("/test")
	if handler == nil {
		t.Fatal("Handler not found")
	}
}

func TestHandle(t *testing.T) {
	r := NewRouter()
	h := SetupHandlerFunc(func(w SetupResponseWriter, req *SetupRequest) {})
	r.Handle("/test", h)
	handler := r.Handler("/test")
	if handler == nil {
		t.Fatal("Handler not found")
	}
}

func TestRegisterPanic(t *testing.T) {
	r := NewRouter()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for empty path")
		}
	}()
	r.register("", nil)
}

func TestRegisterPanicPath(t *testing.T) {
	r := NewRouter()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for path not starting with /")
		}
	}()
	r.register("test", nil)
}

func TestRegisterPanicNilHandler(t *testing.T) {
	r := NewRouter()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil handler")
		}
	}()
	r.register("/test", nil)
}

func TestRegisterPanicNilFunc(t *testing.T) {
	r := NewRouter()
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for nil function")
		}
	}()
	r.HandleFunc("/test", nil)
}

func TestSetupRouter_ServeMOQ_WithHandler(t *testing.T) {
	r := NewRouter()
	called := false
	r.HandleFunc("/test", func(w SetupResponseWriter, req *SetupRequest) {
		called = true
	})

	req := &SetupRequest{Path: "/test"}

	r.ServeMOQ(nil, req) // w is not used in handler

	assert.True(t, called)
}

func TestPackageHandleFunc(t *testing.T) {
	// Test package-level HandleFunc
	HandleFunc("/test", func(w SetupResponseWriter, req *SetupRequest) {
		// dummy
	})
	// Check if handler is registered in DefaultRouter
	handler := DefaultRouter.Handler("/test")
	assert.NotNil(t, handler)
}

func TestPackageHandle(t *testing.T) {
	// Test package-level Handle
	h := SetupHandlerFunc(func(w SetupResponseWriter, req *SetupRequest) {
		// dummy
	})
	Handle("/test2", h)
	handler := DefaultRouter.Handler("/test2")
	assert.NotNil(t, handler)
}

func TestSetupRequest_Context(t *testing.T) {
	ctx := context.Background()
	req := &SetupRequest{
		reqCtx: ctx,
	}
	assert.Equal(t, ctx, req.Context())
}
