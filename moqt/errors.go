package moqt

import (
	"errors"
	"fmt"

	"github.com/okdaichi/gomoqt/transport"
)

var (
	// ErrInvalidScheme is returned when a URL scheme is not supported.
	// Only "https" (for WebTransport) and "moqt" (for QUIC) schemes are valid.
	ErrInvalidScheme = errors.New("moqt: invalid scheme")

	// ErrClosedSession is returned when attempting to use a closed session.
	ErrClosedSession = errors.New("moqt: closed session")

	// ErrServerClosed is returned when the server has been closed.
	ErrServerClosed = errors.New("moqt: server closed")
)

/*
 * Announce Errors
 */

// AnnounceErrorCode represents error codes for track announcement operations.
// These codes are used when an announcement is rejected or fails.
type AnnounceErrorCode uint32

const (
	AnnounceErrorCodeInternal AnnounceErrorCode = 0x0

	// Subscriber-side errors.
	AnnounceErrorCodeDuplicated    AnnounceErrorCode = 0x1
	AnnounceErrorCodeInvalidStatus AnnounceErrorCode = 0x2
	UninterestedErrorCode          AnnounceErrorCode = 0x3

	// Publisher-side errors.
	BannedPrefixErrorCode          AnnounceErrorCode = 0x4
	AnnounceErrorCodeInvalidPrefix AnnounceErrorCode = 0x5
)

// AnnounceErrorText returns a text for the announce error code.
// It returns an empty string if the code is unknown.
func AnnounceErrorText(code AnnounceErrorCode) string {
	switch code {
	case AnnounceErrorCodeInternal:
		return "moqt: internal error"
	case AnnounceErrorCodeDuplicated:
		return "moqt: duplicated broadcast path"
	case AnnounceErrorCodeInvalidStatus:
		return "moqt: invalid announce status"
	case UninterestedErrorCode:
		return "moqt: uninterested"
	case BannedPrefixErrorCode:
		return "moqt: banned prefix"
	case AnnounceErrorCodeInvalidPrefix:
		return "moqt: invalid prefix"
	default:
		return ""
	}
}

// AnnounceError wraps a QUIC stream error with announcement-specific error codes.
type AnnounceError struct{ *transport.StreamError }

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
	SubscribeErrorCodeInternal SubscribeErrorCode = 0x00

	// Range and identity validation errors.
	SubscribeErrorCodeInvalidRange SubscribeErrorCode = 0x01
	SubscribeErrorCodeDuplicateID  SubscribeErrorCode = 0x02
	SubscribeErrorCodeNotFound     SubscribeErrorCode = 0x03
	SubscribeErrorCodeUnauthorized SubscribeErrorCode = 0x04

	// Subscriber-side timeout.
	SubscribeErrorCodeTimeout SubscribeErrorCode = 0x05
)

// SubscribeErrorText returns a text for the subscribe error code.
// It returns an empty string if the code is unknown.
func SubscribeErrorText(code SubscribeErrorCode) string {
	switch code {
	case SubscribeErrorCodeInternal:
		return "moqt: internal error"
	case SubscribeErrorCodeInvalidRange:
		return "moqt: invalid range"
	case SubscribeErrorCodeDuplicateID:
		return "moqt: duplicated id"
	case SubscribeErrorCodeNotFound:
		return "moqt: track does not exist"
	case SubscribeErrorCodeUnauthorized:
		return "moqt: unauthorized"
	case SubscribeErrorCodeTimeout:
		return "moqt: timeout"
	default:
		return ""
	}
}

// SubscribeError wraps a QUIC stream error with subscription-specific error codes.
type SubscribeError struct{ *transport.StreamError }

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

type FetchErrorCode uint32

const (
	FetchErrorCodeInternal FetchErrorCode = 0x00
	FetchErrorCodeTimeout  FetchErrorCode = 0x01
)

func FetchErrorText(code FetchErrorCode) string {
	switch code {
	case FetchErrorCodeInternal:
		return "moqt: internal error"
	case FetchErrorCodeTimeout:
		return "moqt: timeout"
	default:
		return ""
	}
}

type FetchError struct{ *transport.StreamError }

func (err FetchError) Error() string {
	text := FetchErrorText(err.FetchErrorCode())
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

func (err FetchError) FetchErrorCode() FetchErrorCode {
	return FetchErrorCode(err.ErrorCode)
}

type ProbeErrorCode uint32

const (
	ProbeErrorCodeInternal     ProbeErrorCode = 0x00
	ProbeErrorCodeTimeout      ProbeErrorCode = 0x01
	ProbeErrorCodeNotSupported ProbeErrorCode = 0x02
)

func ProbeErrorText(code ProbeErrorCode) string {
	switch code {
	case ProbeErrorCodeInternal:
		return "moqt: internal error"
	case ProbeErrorCodeTimeout:
		return "moqt: timeout"
	case ProbeErrorCodeNotSupported:
		return "moqt: not supported"
	default:
		return ""
	}
}

type ProbeError struct{ *transport.StreamError }

func (err ProbeError) Error() string {
	text := ProbeErrorText(err.ProbeErrorCode())
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

func (err ProbeError) ProbeErrorCode() ProbeErrorCode {
	return ProbeErrorCode(err.ErrorCode)
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
type SessionError struct{ *transport.ApplicationError }

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
	SubscribeCanceledErrorCode  GroupErrorCode = 0x04
	PublishAbortedErrorCode     GroupErrorCode = 0x05
	ClosedSessionGroupErrorCode GroupErrorCode = 0x06
	InvalidSubscribeIDErrorCode GroupErrorCode = 0x07
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
type GroupError struct{ *transport.StreamError }

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
