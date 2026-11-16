package moqt

import (
	"net/http"
	"sync"
)

var _ http.ResponseWriter = (*fakeHTTPResponseWriter)(nil)

// fakeHTTPResponseWriter is a fake implementation of http.ResponseWriter.
// Written bytes and the status code are recorded for assertions.
type fakeHTTPResponseWriter struct {
	mu sync.Mutex

	// writeErr is returned by Write; the zero value means the write succeeds.
	writeErr error

	header     http.Header
	written    []byte
	statusCode int
}

func (m *fakeHTTPResponseWriter) Header() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *fakeHTTPResponseWriter) Write(data []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, data...)
	return len(data), nil
}

func (m *fakeHTTPResponseWriter) WriteHeader(statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusCode = statusCode
}

// Written returns a copy of every byte passed to a successful Write.
func (m *fakeHTTPResponseWriter) Written() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]byte, len(m.written))
	copy(out, m.written)
	return out
}

// StatusCode returns the status code passed to WriteHeader, or 0 if unset.
func (m *fakeHTTPResponseWriter) StatusCode() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusCode
}
