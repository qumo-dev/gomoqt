package moqt

import "crypto/tls"

// ConnectionState describes connection metadata exposed by a MOQ session.
type ConnectionState struct {
	// Protocol version, e.g., "moq-lite-03"
	Version string

	// TLS holds the TLS state when available.
	TLS *tls.ConnectionState
}
