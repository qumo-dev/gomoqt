package moqt

import "github.com/okdaichi/gomoqt/transport"

// Transport-facing aliases to keep public MOQ API surface cohesive while
// preserving the dedicated transport package for cycle avoidance and abstraction.
type (
	StreamConn    = transport.StreamConn
	QUICListener  = transport.QUICListener
	Stream        = transport.Stream
	SendStream    = transport.SendStream
	ReceiveStream = transport.ReceiveStream
	StreamID      = transport.StreamID

	ConnErrorCode        = transport.ConnErrorCode
	ApplicationError     = transport.ApplicationError
	ApplicationErrorCode = transport.ApplicationErrorCode
	StreamError          = transport.StreamError
	StreamErrorCode      = transport.StreamErrorCode
)
