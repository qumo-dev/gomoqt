package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"time"

	quicgo "github.com/quic-go/quic-go"
)

type ConnErrorCode = ApplicationErrorCode

// StreamConn represents a connection that can send and receive streams.
// It abstracts the underlying transport implementation and provides methods for
// creating bidirectional and unidirectional streams.
type StreamConn interface {
	// AcceptStream waits for and accepts the next incoming bidirectional stream.
	AcceptStream(ctx context.Context) (Stream, error)

	// AcceptUniStream waits for and accepts the next incoming unidirectional stream.
	AcceptUniStream(ctx context.Context) (ReceiveStream, error)

	// CloseWithError closes the connection with an error code and message.
	CloseWithError(code ConnErrorCode, msg string) error

	// Context returns the connection's context, which is canceled when the connection is closed.
	Context() context.Context

	// LocalAddr returns the local network address.
	LocalAddr() net.Addr

	// OpenStream opens a new bidirectional stream without blocking.
	OpenStream() (Stream, error)

	// OpenUniStream opens a new unidirectional stream without blocking.
	OpenUniStream() (SendStream, error)

	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr

	// TLS returns the TLS connection state if applicable, or nil if not using TLS.
	TLS() *tls.ConnectionState
}

// Stream is a bidirectional stream that implements both SendStream and ReceiveStream.
type Stream interface {
	SendStream
	ReceiveStream
	// SetDeadline sets the read and write deadlines.
	SetDeadline(time.Time) error
}

// SendStream is a unidirectional stream for sending data.
type SendStream interface {
	io.Writer
	io.Closer

	// CancelWrite cancels writing with the given error code.
	CancelWrite(StreamErrorCode)

	// SetWriteDeadline sets the deadline for write operations.
	SetWriteDeadline(time.Time) error

	// Context returns the stream's context, canceled when the stream is closed.
	Context() context.Context
}

// ReceiveStream is a unidirectional stream for receiving data.
type ReceiveStream interface {
	io.Reader

	// CancelRead cancels reading with the given error code.
	CancelRead(StreamErrorCode)

	// SetReadDeadline sets the deadline for read operations.
	SetReadDeadline(time.Time) error
}

// StreamID uniquely identifies a stream within a connection.
type StreamID = quicgo.StreamID

type ConnectionStats = quicgo.ConnectionStats
