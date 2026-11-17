package moqt

import (
	"errors"
	"fmt"

	"github.com/qumo-dev/gomoqt/transport"
)

var (
	// ErrInvalidScheme is returned when a URL scheme is not supported.
	// Only "https" (for WebTransport) and "moqt" (for QUIC) schemes are valid.
	ErrInvalidScheme = errors.New("moqt: invalid scheme")

	// ErrClosedSession is returned when attempting to use a closed session.
	ErrClosedSession = errors.New("moqt: closed session")

	// ErrServerClosed is returned when the server has been closed.
	ErrServerClosed = errors.New("moqt: server closed")

	// ErrProbeNotSupported is returned by Session.Probe when the peer did
	// not advertise the Probe capability in its SETUP message.
	ErrProbeNotSupported = errors.New("moqt: peer does not support probing")

	// ErrTimestampOutOfRange is returned when a frame's timestamp delta
	// relative to the previous frame cannot be encoded in a 62-bit varint,
	// or when a frame read carries a delta that takes its timestamp below
	// zero. Neither arises from a well-formed publisher; they guard a relay
	// re-publishing frames from a malicious upstream, and a reader of stored
	// frames from a corrupt payload.
	ErrTimestampOutOfRange = errors.New("moqt: frame timestamp delta out of range")
)

/*
 * Error code spaces (moq-lite-06 §4.4)
 *
 * A session close and a stream reset carry codes from two independent
 * spaces; the same number means different things in each. Both spaces share
 * the same ranges:
 *
 *   0x00-0x1f  codes reused unchanged from moq-transport (draft-18+)
 *   0x20-0x2f  reserved; never sent, and never interpreted when received
 *   0x30-0x3f  moq-lite's own (assigned in the stream space only)
 *   0x40-      the application's, opaque to moq-lite
 *
 * gomoqt needs a few stream codes moq-lite does not define (an unauthorized
 * request, a malformed range, ...). They are gomoqt's, so they sit in the
 * application range from 0x40 up; a peer that is not gomoqt treats them as
 * unspecified errors, as §4.4 requires of any code it does not recognize.
 * The typed codes below (AnnounceErrorCode, SubscribeErrorCode, ...) are
 * views of the one stream space: where two kinds share a meaning they share
 * the value.
 */

// Stream error codes defined by moq-lite-06 (Table 3).
const (
	streamInternalError     transport.StreamErrorCode = 0x0
	streamCancelled         transport.StreamErrorCode = 0x1
	streamDeliveryTimeout   transport.StreamErrorCode = 0x2
	streamSessionClosed     transport.StreamErrorCode = 0x3
	streamGoingAway         transport.StreamErrorCode = 0x4
	streamTooFarBehind      transport.StreamErrorCode = 0x5
	streamMalformedTrack    transport.StreamErrorCode = 0x12
	streamNoCapacity        transport.StreamErrorCode = 0x30
	streamControlTimeout    transport.StreamErrorCode = 0x31
	streamGroupTooLarge     transport.StreamErrorCode = 0x32
	streamNotFound          transport.StreamErrorCode = 0x33
	streamOld               transport.StreamErrorCode = 0x34
	streamEvicted           transport.StreamErrorCode = 0x35
	streamUnroutable        transport.StreamErrorCode = 0x36
	streamWrongSize         transport.StreamErrorCode = 0x37
	streamFrameTooLarge     transport.StreamErrorCode = 0x38
	streamTimestampMismatch transport.StreamErrorCode = 0x39
)

// Stream error codes gomoqt assigns in the application range (64+), for
// conditions moq-lite has no code for.
const (
	// streamUnauthorized refuses a request outside the session's grant.
	streamUnauthorized transport.StreamErrorCode = 0x40
	// streamProtocolViolation resets a stream whose peer broke a rule that
	// moq-lite resolves with a stream reset rather than a session close,
	// such as a SUBSCRIBE Frame End without a Group End.
	streamProtocolViolation transport.StreamErrorCode = 0x41
	// streamDuplicateID refuses a SUBSCRIBE that reuses a Subscribe ID.
	streamDuplicateID transport.StreamErrorCode = 0x42
	// streamInvalidPrefix refuses an ANNOUNCE_REQUEST with a malformed prefix.
	streamInvalidPrefix transport.StreamErrorCode = 0x43
	// streamBannedPrefix refuses an ANNOUNCE_REQUEST the publisher bans.
	streamBannedPrefix transport.StreamErrorCode = 0x44
	// streamNotSupported refuses a stream for a capability this endpoint did
	// not advertise, such as a Probe Stream at Probe level None.
	streamNotSupported transport.StreamErrorCode = 0x45
)

// streamCodeText returns the text for a stream error code, or "" when the code
// is not one gomoqt knows.
func streamCodeText(code transport.StreamErrorCode) string {
	switch code {
	case streamInternalError:
		return "moqt: internal error"
	case streamCancelled:
		return "moqt: cancelled"
	case streamDeliveryTimeout:
		return "moqt: delivery timeout"
	case streamSessionClosed:
		return "moqt: session closed"
	case streamGoingAway:
		return "moqt: going away"
	case streamTooFarBehind:
		return "moqt: too far behind"
	case streamMalformedTrack:
		return "moqt: malformed track"
	case streamNoCapacity:
		return "moqt: no capacity"
	case streamControlTimeout:
		return "moqt: control timeout"
	case streamGroupTooLarge:
		return "moqt: group too large"
	case streamNotFound:
		return "moqt: not found"
	case streamOld:
		return "moqt: old group"
	case streamEvicted:
		return "moqt: evicted"
	case streamUnroutable:
		return "moqt: unroutable"
	case streamWrongSize:
		return "moqt: wrong size"
	case streamFrameTooLarge:
		return "moqt: frame too large"
	case streamTimestampMismatch:
		return "moqt: timestamp mismatch"
	case streamUnauthorized:
		return "moqt: unauthorized"
	case streamProtocolViolation:
		return "moqt: protocol violation"
	case streamDuplicateID:
		return "moqt: duplicated id"
	case streamInvalidPrefix:
		return "moqt: invalid prefix"
	case streamBannedPrefix:
		return "moqt: banned prefix"
	case streamNotSupported:
		return "moqt: not supported"
	default:
		return ""
	}
}

/*
 * Announce Errors
 */

// AnnounceErrorCode is a stream error code that resets an Announce Stream.
type AnnounceErrorCode uint32

const (
	AnnounceErrorCodeInternal AnnounceErrorCode = AnnounceErrorCode(streamInternalError)

	// UninterestedErrorCode ends an Announce Stream the subscriber no
	// longer wants: moq-lite's CANCELLED.
	UninterestedErrorCode AnnounceErrorCode = AnnounceErrorCode(streamCancelled)

	// Publisher-side refusals, in gomoqt's application range.
	BannedPrefixErrorCode          AnnounceErrorCode = AnnounceErrorCode(streamBannedPrefix)
	AnnounceErrorCodeInvalidPrefix AnnounceErrorCode = AnnounceErrorCode(streamInvalidPrefix)
	// AnnounceErrorCodeUnauthorized refuses, or ends, announcements for a
	// prefix outside the root of every grant in force on the session.
	AnnounceErrorCodeUnauthorized AnnounceErrorCode = AnnounceErrorCode(streamUnauthorized)
)

// String returns a text for the announce error code.
// It returns an empty string if the code is unknown.
func (code AnnounceErrorCode) String() string {
	if code == UninterestedErrorCode {
		return "moqt: uninterested"
	}
	return streamCodeText(transport.StreamErrorCode(code))
}

// AnnounceError wraps a QUIC stream error with announcement-specific error codes.
type AnnounceError struct{ *transport.StreamError }

// Error returns the text of the announce error code, or the stream error's text
// when the code is unknown.
func (err *AnnounceError) Error() string {
	text := err.AnnounceErrorCode().String()
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

// AnnounceErrorCode returns the announce error code the stream was reset with.
func (err *AnnounceError) AnnounceErrorCode() AnnounceErrorCode {
	return AnnounceErrorCode(err.ErrorCode)
}

/*
 * Subscribe Errors
 */

// SubscribeErrorCode is a stream error code that refuses or ends a
// subscription (and a Track Stream).
type SubscribeErrorCode uint32

const (
	SubscribeErrorCodeInternal SubscribeErrorCode = SubscribeErrorCode(streamInternalError)

	// SubscribeErrorCodeCanceled ends a subscription routinely: moq-lite's
	// CANCELLED, sent by a subscriber that gives up on an unanswered request.
	SubscribeErrorCodeCanceled SubscribeErrorCode = SubscribeErrorCode(streamCancelled)

	// SubscribeErrorCodeNotFound refuses a track or broadcast that is not
	// here: moq-lite's NOT_FOUND.
	SubscribeErrorCodeNotFound SubscribeErrorCode = SubscribeErrorCode(streamNotFound)

	// SubscribeErrorCodeUnroutable refuses a subscription whose only source
	// the subscriber's own Hop ID excludes: moq-lite's UNROUTABLE.
	SubscribeErrorCodeUnroutable SubscribeErrorCode = SubscribeErrorCode(streamUnroutable)

	// SubscribeErrorCodeTimeout ends a request the peer never answered:
	// moq-lite's CONTROL_TIMEOUT.
	SubscribeErrorCodeTimeout SubscribeErrorCode = SubscribeErrorCode(streamControlTimeout)

	// Refusals in gomoqt's application range.
	SubscribeErrorCodeUnauthorized SubscribeErrorCode = SubscribeErrorCode(streamUnauthorized)
	// SubscribeErrorCodeInvalidRange refuses a SUBSCRIBE or SUBSCRIBE_UPDATE
	// whose range breaks a moq-lite rule, such as a Frame End without a
	// Group End (moq-lite-06 §3.6 calls this a protocol violation that
	// resets the stream).
	SubscribeErrorCodeInvalidRange SubscribeErrorCode = SubscribeErrorCode(streamProtocolViolation)
	SubscribeErrorCodeDuplicateID  SubscribeErrorCode = SubscribeErrorCode(streamDuplicateID)
)

// String returns a text for the subscribe error code.
// It returns an empty string if the code is unknown.
func (code SubscribeErrorCode) String() string {
	switch code {
	case SubscribeErrorCodeNotFound:
		return "moqt: track does not exist"
	case SubscribeErrorCodeInvalidRange:
		return "moqt: invalid range"
	case SubscribeErrorCodeTimeout:
		return "moqt: timeout"
	}
	return streamCodeText(transport.StreamErrorCode(code))
}

// SubscribeError wraps a QUIC stream error with subscription-specific error codes.
type SubscribeError struct{ *transport.StreamError }

// Error returns the text of the subscribe error code, or the stream error's text
// when the code is unknown.
func (err *SubscribeError) Error() string {
	text := err.SubscribeErrorCode().String()
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

// SubscribeErrorCode returns the subscribe error code the stream was reset with.
func (err *SubscribeError) SubscribeErrorCode() SubscribeErrorCode {
	return SubscribeErrorCode(err.ErrorCode)
}

// FetchErrorCode is a stream error code that ends a Fetch Stream from the
// requesting side. A publisher's refusal arrives as a *GroupError instead,
// since the answer to a fetch is its group.
type FetchErrorCode uint32

const (
	FetchErrorCodeInternal FetchErrorCode = FetchErrorCode(streamInternalError)
	// FetchErrorCodeTimeout is moq-lite's CONTROL_TIMEOUT.
	FetchErrorCodeTimeout FetchErrorCode = FetchErrorCode(streamControlTimeout)
)

// String returns a text for the fetch error code.
// It returns an empty string if the code is unknown.
func (code FetchErrorCode) String() string {
	if code == FetchErrorCodeTimeout {
		return "moqt: timeout"
	}
	return streamCodeText(transport.StreamErrorCode(code))
}

type FetchError struct{ *transport.StreamError }

// Error returns the text of the fetch error code, or the stream error's text
// when the code is unknown.
func (err *FetchError) Error() string {
	text := err.FetchErrorCode().String()
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

// FetchErrorCode returns the fetch error code the stream was reset with.
func (err *FetchError) FetchErrorCode() FetchErrorCode {
	return FetchErrorCode(err.ErrorCode)
}

// ProbeErrorCode is a stream error code that resets a Probe Stream.
type ProbeErrorCode uint32

const (
	ProbeErrorCodeInternal ProbeErrorCode = ProbeErrorCode(streamInternalError)
	// ProbeErrorCodeTimeout is moq-lite's CONTROL_TIMEOUT.
	ProbeErrorCodeTimeout ProbeErrorCode = ProbeErrorCode(streamControlTimeout)
	// ProbeErrorCodeNotSupported resets a Probe Stream opened against an
	// endpoint that advertised Probe level None (gomoqt's application range).
	ProbeErrorCodeNotSupported ProbeErrorCode = ProbeErrorCode(streamNotSupported)
)

// String returns a text for the probe error code.
// It returns an empty string if the code is unknown.
func (code ProbeErrorCode) String() string {
	if code == ProbeErrorCodeTimeout {
		return "moqt: timeout"
	}
	return streamCodeText(transport.StreamErrorCode(code))
}

type ProbeError struct{ *transport.StreamError }

// Error returns the text of the probe error code, or the stream error's text
// when the code is unknown.
func (err *ProbeError) Error() string {
	text := err.ProbeErrorCode().String()
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

// ProbeErrorCode returns the probe error code the stream was reset with.
func (err *ProbeError) ProbeErrorCode() ProbeErrorCode {
	return ProbeErrorCode(err.ErrorCode)
}

/*
 * Session Error
 */

// SessionErrorCode is a session error code (moq-lite-06 Table 2), sent when
// closing the session. Codes from 0x40 up are the application's.
type SessionErrorCode uint32

const (
	NoError SessionErrorCode = 0x0

	InternalSessionErrorCode     SessionErrorCode = 0x1
	UnauthorizedSessionErrorCode SessionErrorCode = 0x2
	ProtocolViolationErrorCode   SessionErrorCode = 0x3
	// KeyValueFormattingErrorCode closes a session whose peer sent a
	// malformed key-value pair, or repeated one more than allowed.
	KeyValueFormattingErrorCode SessionErrorCode = 0x6
	GoAwayTimeoutErrorCode      SessionErrorCode = 0x10
	// ControlMessageTimeoutErrorCode closes a session whose peer took too
	// long to respond to a control message.
	ControlMessageTimeoutErrorCode SessionErrorCode = 0x11
	// VersionNegotiationFailedErrorCode closes a session for which no
	// version could be negotiated.
	VersionNegotiationFailedErrorCode SessionErrorCode = 0x15

	// NotSupportedErrorCode refuses a token, or a grant, that the acceptor
	// cannot verify or express (see AuthWriter). The Auth Stream is a gomoqt
	// extension, so the code sits in the application range.
	NotSupportedErrorCode SessionErrorCode = 0x40
)

// String returns a text for the session error code.
// It returns an empty string if the code is unknown.
func (code SessionErrorCode) String() string {
	switch code {
	case NoError:
		return "moqt: no error"
	case InternalSessionErrorCode:
		return "moqt: internal error"
	case UnauthorizedSessionErrorCode:
		return "moqt: unauthorized"
	case ProtocolViolationErrorCode:
		return "moqt: protocol violation"
	case KeyValueFormattingErrorCode:
		return "moqt: key-value formatting error"
	case GoAwayTimeoutErrorCode:
		return "moqt: goaway timeout"
	case ControlMessageTimeoutErrorCode:
		return "moqt: control message timeout"
	case VersionNegotiationFailedErrorCode:
		return "moqt: version negotiation failed"
	case NotSupportedErrorCode:
		return "moqt: not supported"
	default:
		return ""
	}
}

// SessionError wraps a QUIC application error with session-specific error codes.
type SessionError struct{ *transport.ApplicationError }

// Error returns the text of the session error code and whether the local or
// the remote endpoint closed the session, or the application error's text
// when the code is unknown.
func (err *SessionError) Error() string {
	var role string
	if err.Remote {
		role = "remote"
	} else {
		role = "local"
	}
	text := err.SessionErrorCode().String()
	if text != "" {
		return fmt.Sprintf("%s (%s)", text, role)
	}
	return err.ApplicationError.Error()
}

// SessionErrorCode returns the session error code the session was closed with.
func (err *SessionError) SessionErrorCode() SessionErrorCode {
	return SessionErrorCode(err.ErrorCode)
}

/*
 * Group Error
 */

// GroupErrorCode is a stream error code that resets a Group Stream, or the
// group a Fetch Stream answers with.
type GroupErrorCode uint32

const (
	InternalGroupErrorCode GroupErrorCode = GroupErrorCode(streamInternalError)

	// SubscribeCanceledErrorCode is moq-lite's CANCELLED: the group is no
	// longer wanted, by either end.
	SubscribeCanceledErrorCode GroupErrorCode = GroupErrorCode(streamCancelled)
	// ExpiredGroupErrorCode is moq-lite's DELIVERY_TIMEOUT: the group aged
	// out of the subscriber's Max Age before it was delivered.
	ExpiredGroupErrorCode GroupErrorCode = GroupErrorCode(streamDeliveryTimeout)
	// ClosedSessionGroupErrorCode is moq-lite's SESSION_CLOSED.
	ClosedSessionGroupErrorCode GroupErrorCode = GroupErrorCode(streamSessionClosed)
	// GroupTooLargeErrorCode aborts a group that grew past the publisher's
	// cache budget.
	GroupTooLargeErrorCode GroupErrorCode = GroupErrorCode(streamGroupTooLarge)
	// NotFoundGroupErrorCode refuses a group that was never here, such as a
	// fetch of a group the publisher does not hold.
	NotFoundGroupErrorCode GroupErrorCode = GroupErrorCode(streamNotFound)
	// OldGroupErrorCode refuses a group superseded by a newer one and dropped.
	OldGroupErrorCode GroupErrorCode = GroupErrorCode(streamOld)
	// EvictedGroupErrorCode refuses a group dropped under memory pressure
	// while still current; unlike an old one it may be fetched again.
	EvictedGroupErrorCode GroupErrorCode = GroupErrorCode(streamEvicted)
	// FrameTooLargeErrorCode resets a group with a frame declaring a payload
	// larger than the receiver accepts.
	FrameTooLargeErrorCode GroupErrorCode = GroupErrorCode(streamFrameTooLarge)
	// InvalidRangeGroupErrorCode refuses a fetch whose frame range breaks a
	// moq-lite rule (Frame End below Frame Start), or one the publisher cannot
	// serve in full (gomoqt's application range).
	InvalidRangeGroupErrorCode GroupErrorCode = GroupErrorCode(streamProtocolViolation)
	// UnauthorizedGroupErrorCode refuses a group the session's scope does not
	// cover, such as the one a FETCH outside it asks for (see AuthHandler).
	UnauthorizedGroupErrorCode GroupErrorCode = GroupErrorCode(streamUnauthorized)
)

// String returns a text for the group error code.
// It returns an empty string if the code is unknown.
func (code GroupErrorCode) String() string {
	switch code {
	case SubscribeCanceledErrorCode:
		return "moqt: subscribe canceled"
	case ExpiredGroupErrorCode:
		return "moqt: group expires"
	case InvalidRangeGroupErrorCode:
		return "moqt: invalid range"
	}
	return streamCodeText(transport.StreamErrorCode(code))
}

// GroupError wraps a QUIC stream error with group-specific error codes.
type GroupError struct{ *transport.StreamError }

// Error returns the text of the group error code, or the stream error's text
// when the code is unknown.
func (err *GroupError) Error() string {
	text := err.GroupErrorCode().String()
	if text != "" {
		return text
	}
	return err.StreamError.Error()
}

// GroupErrorCode returns the group error code the stream was reset with.
func (err *GroupError) GroupErrorCode() GroupErrorCode {
	return GroupErrorCode(err.ErrorCode)
}
