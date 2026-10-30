package moqt

import (
	"time"
)

// Config contains configuration options for MOQ sessions.
type Config struct {
	// SetupTimeout is the maximum time to wait for session setup to complete.
	// If zero, a default timeout of 5 seconds is used.
	SetupTimeout time.Duration

	// ProbeInterval is the ticker period for the publisher-side probe loop.
	// If zero, defaults to 100ms.
	ProbeInterval time.Duration

	// ProbeMaxAge is the maximum interval between probe sends regardless of
	// bitrate change. If zero, defaults to 10s.
	ProbeMaxAge time.Duration

	// ProbeMaxDelta is the fractional change threshold (0.0–1.0) that triggers
	// an early probe send before ProbeMaxAge elapses.
	// If zero, defaults to 0.10 (10%).
	ProbeMaxDelta float64
}

// setupTimeout returns the configured setup timeout or a default value.
func (c *Config) setupTimeout() time.Duration {
	if c != nil && c.SetupTimeout > 0 {
		return c.SetupTimeout
	}
	return 5 * time.Second
}

// probeInterval returns the configured probe interval or the default (100ms).
func (c *Config) probeInterval() time.Duration {
	if c != nil && c.ProbeInterval > 0 {
		return c.ProbeInterval
	}
	return 100 * time.Millisecond
}

// probeMaxAge returns the configured probe max age or the default (10s).
func (c *Config) probeMaxAge() time.Duration {
	if c != nil && c.ProbeMaxAge > 0 {
		return c.ProbeMaxAge
	}
	return 10 * time.Second
}

// probeMaxDelta returns the configured probe max delta or the default (0.10).
func (c *Config) probeMaxDelta() float64 {
	if c != nil && c.ProbeMaxDelta > 0 {
		return c.ProbeMaxDelta
	}
	return 0.10
}

// Clone creates a copy of the Config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	return &Config{
		SetupTimeout:  c.SetupTimeout,
		ProbeInterval: c.ProbeInterval,
		ProbeMaxAge:   c.ProbeMaxAge,
		ProbeMaxDelta: c.ProbeMaxDelta,
	}
}
