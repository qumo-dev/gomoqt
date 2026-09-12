package moqt

import (
	"net/http"
	"sync"
)

var _ http.ResponseWriter = (*FakeHTTPResponseWriter)(nil)

// FakeHTTPResponseWriter is a fake implementation of http.ResponseWriter.
// Written bytes and the status code are recorded for assertions.
type FakeHTTPResponseWriter struct {
	mu sync.Mutex

	// WriteErr is returned by Write; the zero value means the write succeeds.
	WriteErr error

	header     http.Header
	written    []byte
	statusCode int
}

func (m *FakeHTTPResponseWriter) Header() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *FakeHTTPResponseWriter) Write(data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteErr != nil {
		return 0, m.WriteErr
	}
	m.written = append(m.written, data...)
	return len(data), nil
}

func (m *FakeHTTPResponseWriter) WriteHeader(statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCode = statusCode
}

// Written returns a copy of every byte passed to a successful Write.
func (m *FakeHTTPResponseWriter) Written() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.written))
	copy(out, m.written)
	return out
}

// StatusCode returns the status code passed to WriteHeader, or 0 if unset.
func (m *FakeHTTPResponseWriter) StatusCode() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusCode
}
