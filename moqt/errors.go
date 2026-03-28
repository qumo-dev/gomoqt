package moqt

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidScheme is returned when a URL scheme is not supported.
	// Only "https" (for WebTransport) and "moqt" (for QUIC) schemes are valid.
	ErrInvalidScheme = errors.New("moqt: invalid scheme")

	// ErrClosedSession is returned when attempting to use a closed session.
	ErrClosedSession = errors.New("moqt: closed session")

	// ErrServerClosed is returned when the server has been closed.
	ErrServerClosed = errors.New("moqt: server closed")

	// ErrClientClosed is returned when the client has been closed.
	ErrClientClosed = errors.New("moqt: client closed")
)

/*
 * Announce Errors
 */

// AnnounceErrorCode represents error codes for track announcement operations.
// These codes are used when an announcement is rejected or fails.
type AnnounceErrorCode uint32

const (
	InternalAnnounceErrorCode AnnounceErrorCode = 0x0

	// Subscriber
	DuplicatedAnnounceErrorCode    AnnounceErrorCode = 0x1
	InvalidAnnounceStatusErrorCode AnnounceErrorCode = 0x2 // TODO: Is this necessary?
	UninterestedErrorCode          AnnounceErrorCode = 0x3

	// Publisher
	BannedPrefixErrorCode  AnnounceErrorCode = 0x4 // TODO: Is this necessary?
	InvalidPrefixErrorCode AnnounceErrorCode = 0x5 // TODO: Is this necessary?
)

// AnnounceErrorText returns a text for the announce error code.
// It returns an empty string if the code is unknown.
func AnnounceErrorText(code AnnounceErrorCode) string {
	switch code {
	case InternalAnnounceErrorCode:
		return "moqt: internal error"
	case DuplicatedAnnounceErrorCode:
		return "moqt: duplicated broadcast path"
	case InvalidAnnounceStatusErrorCode:
		return "moqt: invalid announce status"
	case UninterestedErrorCode:
		return "moqt: uninterested"
	case BannedPrefixErrorCode:
		return "moqt: banned prefix"
	case InvalidPrefixErrorCode:
		return "moqt: invalid prefix"
	default:
		return ""
	}
}

// AnnounceError wraps a QUIC stream error with announcement-specific error codes.
type AnnounceError struct{ *StreamError }

func (err AnnounceError) Error() string {
	text := AnnounceErrorText(err.AnnounceErrorCode())
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

func (err AnnounceError) AnnounceErrorCode() AnnounceErrorCode {
	return AnnounceErrorCode(err.ErrorCode)
}

/*
 * Subscribe Errors
 */

// SubscribeErrorCode represents error codes for subscription operations.
// These codes are used when a subscription request is rejected or fails.
type SubscribeErrorCode uint32

const (
	InternalSubscribeErrorCode SubscribeErrorCode = 0x00

	// Error code used internally, basically not used in the application.
	// These error codes are used before subscribe negotiation is completed,

	//
	InvalidRangeErrorCode SubscribeErrorCode = 0x01
	//
	DuplicateSubscribeIDErrorCode SubscribeErrorCode = 0x02
	//
	TrackNotFoundErrorCode SubscribeErrorCode = 0x03
	//
	UnauthorizedSubscribeErrorCode SubscribeErrorCode = 0x04 // TODO: Is this necessary?
	// Subscriber
	SubscribeTimeoutErrorCode SubscribeErrorCode = 0x05
	// ClosedTrackErrorCode           SubscribeErrorCode = 0x07 // TODO: Is this necessary?

	// Error code used by the application.
	// These error codes are used after subscribe negotiation is completed.
)

// SubscribeErrorText returns a text for the subscribe error code.
// It returns an empty string if the code is unknown.
func SubscribeErrorText(code SubscribeErrorCode) string {
	switch code {
	case InternalSubscribeErrorCode:
		return "moqt: internal error"
	case InvalidRangeErrorCode:
		return "moqt: invalid range"
	case DuplicateSubscribeIDErrorCode:
		return "moqt: duplicated id"
	case TrackNotFoundErrorCode:
		return "moqt: track does not exist"
	case UnauthorizedSubscribeErrorCode:
		return "moqt: unauthorized"
	case SubscribeTimeoutErrorCode:
		return "moqt: timeout"
	default:
		return ""
	}
}

// SubscribeError wraps a QUIC stream error with subscription-specific error codes.
type SubscribeError struct{ *StreamError }

func (err SubscribeError) Error() string {
	text := SubscribeErrorText(err.SubscribeErrorCode())
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

func (err SubscribeError) SubscribeErrorCode() SubscribeErrorCode {
	return SubscribeErrorCode(err.ErrorCode)
}

/*
 * Session Error
 */

// SessionErrorCode represents error codes for MOQ session operations.
// These codes are used at the connection level for protocol errors.
type SessionErrorCode uint32

const (
	NoError SessionErrorCode = 0x0

	InternalSessionErrorCode         SessionErrorCode = 0x1
	UnauthorizedSessionErrorCode     SessionErrorCode = 0x2
	ProtocolViolationErrorCode       SessionErrorCode = 0x3
	ParameterLengthMismatchErrorCode SessionErrorCode = 0x5
	TooManySubscribeErrorCode        SessionErrorCode = 0x6
	GoAwayTimeoutErrorCode           SessionErrorCode = 0x10
	UnsupportedVersionErrorCode      SessionErrorCode = 0x12

	SetupFailedErrorCode SessionErrorCode = 0x13
)

// SessionErrorText returns a text for the session error code.
// It returns an empty string if the code is unknown.
func SessionErrorText(code SessionErrorCode) string {
	switch code {
	case NoError:
		return "moqt: no error"
	case InternalSessionErrorCode:
		return "moqt: internal error"
	case UnauthorizedSessionErrorCode:
		return "moqt: unauthorized"
	case ProtocolViolationErrorCode:
		return "moqt: protocol violation"
	case ParameterLengthMismatchErrorCode:
		return "moqt: parameter length mismatch"
	case TooManySubscribeErrorCode:
		return "moqt: too many subscribes"
	case GoAwayTimeoutErrorCode:
		return "moqt: goaway timeout"
	case UnsupportedVersionErrorCode:
		return "moqt: unsupported version"
	case SetupFailedErrorCode:
		return "moqt: setup failed"
	default:
		return ""
	}
}

// SessionError wraps a QUIC application error with session-specific error codes.
type SessionError struct{ *ApplicationError }

func (err SessionError) Error() string {
	var role string
	if err.Remote {
		role = "remote"
	} else {
		role = "local"
	}
	text := SessionErrorText(err.SessionErrorCode())
	if text != "" {
		return fmt.Sprintf("%s (%s)", text, role)
	}
	return err.ApplicationError.Error()
}

func (err SessionError) SessionErrorCode() SessionErrorCode {
	return SessionErrorCode(err.ErrorCode)
}

/*
 * Group Error
 */

// GroupErrorCode represents error codes for group operations.
type GroupErrorCode uint32

const (
	InternalGroupErrorCode GroupErrorCode = 0x00

	OutOfRangeErrorCode         GroupErrorCode = 0x02
	ExpiredGroupErrorCode       GroupErrorCode = 0x03
	SubscribeCanceledErrorCode  GroupErrorCode = 0x04 // TODO: Is this necessary?
	PublishAbortedErrorCode     GroupErrorCode = 0x05
	ClosedSessionGroupErrorCode GroupErrorCode = 0x06
	InvalidSubscribeIDErrorCode GroupErrorCode = 0x07 // TODO: Is this necessary?
)

// GroupErrorText returns a text for the group error code.
// It returns an empty string if the code is unknown.
func GroupErrorText(code GroupErrorCode) string {
	switch code {
	case InternalGroupErrorCode:
		return "moqt: internal error"
	case OutOfRangeErrorCode:
		return "moqt: out of range"
	case ExpiredGroupErrorCode:
		return "moqt: group expires"
	case SubscribeCanceledErrorCode:
		return "moqt: subscribe canceled"
	case PublishAbortedErrorCode:
		return "moqt: publish aborted"
	case ClosedSessionGroupErrorCode:
		return "moqt: session closed"
	case InvalidSubscribeIDErrorCode:
		return "moqt: invalid subscribe id"
	default:
		return ""
	}
}

// GroupError wraps a QUIC stream error with group-specific error codes.
type GroupError struct{ *StreamError }

func (err GroupError) Error() string {
	text := GroupErrorText(err.GroupErrorCode())
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

func (err GroupError) GroupErrorCode() GroupErrorCode {
	return GroupErrorCode(err.ErrorCode)
}
