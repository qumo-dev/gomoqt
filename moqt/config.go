package moqt

import (
	"time"
)

// Config contains configuration options for MOQ sessions.
type Config struct {
	// SetupTimeout is the maximum time to wait for session setup to complete.
	// If zero, a default timeout of 5 seconds is used.
	SetupTimeout time.Duration
}

// setupTimeout returns the configured setup timeout or a default value.
func (c *Config) setupTimeout() time.Duration {
	if c != nil && c.SetupTimeout > 0 {
		return c.SetupTimeout
	}
	return 5 * time.Second
}

// Clone creates a copy of the Config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	return &Config{
		SetupTimeout: c.SetupTimeout,
	}
}
