package transport

import (
	quicgo "github.com/quic-go/quic-go"
)

// TransportError represents a QUIC transport layer error.
type TransportError = quicgo.TransportError

// ApplicationError represents an application-level error in QUIC.
type ApplicationError = quicgo.ApplicationError

// VersionNegotiationError occurs when version negotiation fails.
type VersionNegotiationError = quicgo.VersionNegotiationError

// StatelessResetError indicates that a stateless reset was received.
type StatelessResetError = quicgo.StatelessResetError

// IdleTimeoutError indicates that the connection timed out due to inactivity.
type IdleTimeoutError = quicgo.IdleTimeoutError

// HandshakeTimeoutError indicates that the handshake did not complete in time.
type HandshakeTimeoutError = quicgo.HandshakeTimeoutError

// Error codes for QUIC transport, application, and stream operations.
type (
	// TransportErrorCode identifies transport-layer protocol errors.
	TransportErrorCode = quicgo.TransportErrorCode
	// ApplicationErrorCode identifies application-defined errors.
	ApplicationErrorCode = quicgo.ApplicationErrorCode
	// StreamErrorCode identifies stream-specific errors.
	StreamErrorCode = quicgo.StreamErrorCode
)

const (
	NoError                   TransportErrorCode = quicgo.NoError
	InternalError             TransportErrorCode = quicgo.InternalError
	ConnectionRefused         TransportErrorCode = quicgo.ConnectionRefused
	FlowControlError          TransportErrorCode = quicgo.FlowControlError
	StreamLimitError          TransportErrorCode = quicgo.StreamLimitError
	StreamStateError          TransportErrorCode = quicgo.StreamStateError
	FinalSizeError            TransportErrorCode = quicgo.FinalSizeError
	FrameEncodingError        TransportErrorCode = quicgo.FrameEncodingError
	TransportParameterError   TransportErrorCode = quicgo.TransportParameterError
	ConnectionIDLimitError    TransportErrorCode = quicgo.ConnectionIDLimitError
	ProtocolViolation         TransportErrorCode = quicgo.ProtocolViolation
	InvalidToken              TransportErrorCode = quicgo.InvalidToken
	ApplicationErrorErrorCode TransportErrorCode = quicgo.ApplicationErrorErrorCode
	CryptoBufferExceeded      TransportErrorCode = quicgo.CryptoBufferExceeded
	KeyUpdateError            TransportErrorCode = quicgo.KeyUpdateError
	AEADLimitReached          TransportErrorCode = quicgo.AEADLimitReached
	NoViablePathError         TransportErrorCode = quicgo.NoViablePathError
)

// StreamError is used for Stream.CancelRead and Stream.CancelWrite.
// It is also returned from Stream.Read and Stream.Write if the peer canceled reading or writing.
type StreamError = quicgo.StreamError

// DatagramTooLargeError is returned from Connection.SendDatagram if the payload is too large to be sent.
type DatagramTooLargeError = quicgo.DatagramTooLargeError
