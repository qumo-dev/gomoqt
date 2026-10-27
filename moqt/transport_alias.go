package moqt

import "github.com/qumo-dev/gomoqt/transport"

// Transport-facing aliases to keep public MOQ API surface cohesive while
// preserving the dedicated transport package for cycle avoidance and abstraction.
type (
	StreamConn   = transport.StreamConn
	QUICListener = transport.QUICListener

	QUICConfig    = transport.QUICConfig
	QUICTransport = transport.QUICTransport
	QUICPath      = transport.QUICPath

	WebTransportSession = transport.WebTransportSession
)
