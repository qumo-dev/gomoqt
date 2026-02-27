package moqt

import (
	"github.com/stretchr/testify/mock"
)

var _ SetupResponseWriter = (*MockSetupResponseWriter)(nil)

// MockSetupResponseWriter is a mock implementation of SetupResponseWriter interface
type MockSetupResponseWriter struct {
	mock.Mock
}

func (m *MockSetupResponseWriter) SelectVersion(v Version) error {
	args := m.Called(v)
	return args.Error(0)
}

func (m *MockSetupResponseWriter) SetExtensions(extensions *Extension) {
	m.Called(extensions)
}

func (m *MockSetupResponseWriter) Version() Version {
	args := m.Called()
	return args.Get(0).(Version)
}

func (m *MockSetupResponseWriter) Extensions() *Extension {
	args := m.Called()
	if ext, ok := args.Get(0).(*Extension); ok {
		return ext
	}
	return nil
}

// Accept is provided purely for convenience in some tests; not part of the
// SetupResponseWriter interface.
func (m *MockSetupResponseWriter) Accept(mux *TrackMux) (*Session, error) {
	args := m.Called(mux)
	return args.Get(0).(*Session), args.Error(1)
}

func (m *MockSetupResponseWriter) Reject(code SessionErrorCode) {
	m.Called(code)
}
