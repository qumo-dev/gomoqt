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

	// OpenStream opens a new bidirectional stream without blocking, returning a
	// StreamLimitReachedError (wrapped by the transport) when the peer's
	// MAX_STREAMS credit is exhausted.
	//
	// Deprecated: Use OpenStreamSync, which backpressures on the peer's
	// stream-limit flow control instead of failing. All of gomoqt's outgoing
	// streams use the Sync variant; this non-blocking form has no production
	// callers and remains only for parity with quic-go.
	OpenStream() (Stream, error)

	// OpenStreamSync opens a new bidirectional stream, blocking until the peer's
	// stream-limit flow control grants one (or ctx is canceled). Prefer this over
	// OpenStream when opening a stream should backpressure rather than fail on a
	// transient stream-limit condition — e.g. opening MoQ control streams
	// (subscribe, fetch, announce, probe) and GOAWAY.
	OpenStreamSync(ctx context.Context) (Stream, error)

	// OpenUniStream opens a new unidirectional stream without blocking,
	// returning a StreamLimitReachedError (wrapped by the transport) when the
	// peer's MAX_STREAMS credit is exhausted.
	//
	// Deprecated: Use OpenUniStreamSync, which backpressures on the peer's
	// stream-limit flow control instead of failing. gomoqt opens group streams
	// via OpenUniStreamSync; this non-blocking form has no production callers
	// and remains only for parity with quic-go.
	OpenUniStream() (SendStream, error)

	// OpenUniStreamSync opens a new unidirectional stream, blocking until the
	// peer's stream-limit flow control grants one (or ctx is canceled). Prefer
	// this when opening a stream should backpressure rather than fail — e.g.
	// opening MoQ group streams in a publish loop, where a stream-limit error
	// would otherwise abort the whole publisher.
	OpenUniStreamSync(ctx context.Context) (SendStream, error)

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
